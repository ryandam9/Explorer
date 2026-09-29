package vpctui

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// VPC architecture diagram (SVG)
//
// vpcDiagramSVG renders a deterministic, hand-laid-out SVG architecture diagram
// of a VPC, drawn the way an AWS architecture diagram is: a dashed cloud
// boundary with the region on it, the VPC inside, dashed availability-zone
// columns, and subnet cards colour-coded public / private / isolated by their
// default route. It is a pure function over the export snapshot (no AWS calls,
// no AI), so it is stable for a given input and unit-testable.
//
// What the diagram is for is the *relationships*, not the inventory — the
// tables below the diagram already list every resource. So each subnet card
// carries the things bound to it (its route table, its network ACL, the
// interface endpoints with an ENI in it, a NAT gateway living in it), and
// everything the VPC reaches out to — internet gateway, peerings, transit
// gateway attachments, VPN gateways, gateway endpoints — is a node on the edge
// rail with a line from every subnet whose route table actually routes to it.
// A line therefore means "this subnet's route table has a route to that", which
// is the question a VPC diagram exists to answer.
//
// The SVG carries no colours of its own: every fill and stroke comes from a
// class in assets/report.css bound to a theme token, so the diagram follows the
// report's light/dark mode and prints legibly. Toggleable parts are wrapped in
// <g data-layer="…"> groups (subnets, labels, sg, nat, routing, endpoints,
// peering, nacl, traffic) that the report's checkbox bar shows and hides in
// pure CSS. The output is a standalone <svg> (carries its own xmlns), so it
// embeds inline in the HTML report and writes to a .svg file as-is — where,
// having no stylesheet, it falls back to the neutral defaults declared on the
// root element.
//
// Tall AZ columns wrap into lanes (dgMaxRows per lane) so a 40-subnet VPC stays
// a readable grid instead of one extreme ribbon.
// ---------------------------------------------------------------------------

// Diagram geometry (pixels).
const (
	dgMargin      = 26
	dgCloudPad    = 18
	dgCloudHeader = 28
	dgInternetW   = 156
	dgInternetH   = 44
	dgIGWW        = 168
	dgIGWH        = 42
	dgGapNetIGW   = 30
	dgGapIGWVPC   = 32
	dgVPCHeader   = 38
	dgVPCPad      = 20
	dgAZHeader    = 26
	dgAZPad       = 12
	dgAZGap       = 20
	dgLaneGap     = 14
	dgSubnetW     = 268
	dgSubnetH     = 152
	dgSubnetGap   = 16
	dgChipH       = 18
	dgRailW       = 216
	dgRailH       = 60
	dgRailGap     = 16
	dgGapVPCRail  = 34
	dgLegendH     = 58
	dgMaxRows     = 8 // subnets per lane before an AZ column wraps into another lane
)

// dgEgress is a subnet's default-route (0.0.0.0/0) destination.
type dgEgress struct {
	kind   string // "igw" public · "nat" private · "other" · "none" isolated
	target string
}

// placedSubnet is a subnet box with its computed position.
type placedSubnet struct {
	info   SubnetInfo
	egress dgEgress
	enis   int
	sgs    []string
	nat    *NatGWInfo
	rt     string   // the route table associated with it (or the main table)
	nacl   string   // the network ACL associated with it
	epIDs  []string // interface endpoints with an ENI in it
	x, y   int
	gutter int // x of the free channel left of this subnet's AZ column
}

// railNode is something outside the VPC that subnets route to: a peering, a
// transit-gateway attachment, a VPN gateway, or a gateway endpoint.
type railNode struct {
	id    string
	kind  string // "peering" | "tgw" | "vgw" | "endpoint"
	layer string // the data-layer group it belongs to
	title string
	sub   string
	x, y  int
}

// vpcDiagramSVG builds the architecture diagram for the export.
func vpcDiagramSVG(data fullExport) string {
	snap := data.Snap
	egress := subnetEgress(snap)
	eniCount := eniCountBySubnet(snap)
	sgMap := subnetSGs(snap)
	rtBySubnet, rtByID := routeTablesBySubnet(snap)
	naclBySubnet := naclsBySubnet(snap)
	epBySubnet := interfaceEndpointsBySubnet(snap)

	natBySubnet := map[string]NatGWInfo{}
	for i := range snap.NatGateways {
		if snap.NatGateways[i].SubnetID != "" {
			natBySubnet[snap.NatGateways[i].SubnetID] = snap.NatGateways[i]
		}
	}

	// Group subnets by AZ; order AZs, and within an AZ order public → private →
	// other → isolated, then by ID, for a stable, readable layout.
	byAZ := map[string][]SubnetInfo{}
	for _, s := range snap.Subnets {
		az := s.AZ
		if az == "" {
			az = "(no AZ)"
		}
		byAZ[az] = append(byAZ[az], s)
	}
	azs := make([]string, 0, len(byAZ))
	for az := range byAZ {
		azs = append(azs, az)
	}
	sort.Strings(azs)
	for _, az := range azs {
		col := byAZ[az]
		sort.SliceStable(col, func(i, j int) bool {
			ri, rj := egressRank(egress[col[i].ID].kind), egressRank(egress[col[j].ID].kind)
			if ri != rj {
				return ri < rj
			}
			return col[i].ID < col[j].ID
		})
		byAZ[az] = col
	}

	hasIGW := len(snap.InternetGateways) > 0
	igwID := ""
	if hasIGW {
		igwID = snap.InternetGateways[0].ID
	}
	rail := buildRailNodes(data)

	// ---- layout pass 1: lane counts + widths ----------------------------
	azLanes := map[string]int{}
	maxRows := 0
	totalInnerW := 0
	for i, az := range azs {
		n := len(byAZ[az])
		lanes := 1
		if n > 0 {
			lanes = (n + dgMaxRows - 1) / dgMaxRows
		}
		azLanes[az] = lanes
		rows := 0
		if n > 0 {
			rows = (n + lanes - 1) / lanes
		}
		if rows > maxRows {
			maxRows = rows
		}
		azW := lanes*dgSubnetW + (lanes-1)*dgLaneGap + 2*dgAZPad
		if i > 0 {
			totalInnerW += dgAZGap
		}
		totalInnerW += azW
	}
	if len(azs) == 0 {
		totalInnerW = dgSubnetW
	}
	colInnerH := dgAZHeader + dgAZPad
	if maxRows > 0 {
		colInnerH += maxRows*dgSubnetH + (maxRows-1)*dgSubnetGap + dgAZPad
	} else {
		colInnerH += 40
	}

	vpcW := totalInnerW + 2*dgVPCPad
	vpcH := dgVPCHeader + dgVPCPad + colInnerH + dgVPCPad

	// The rail wraps to as many rows as it needs, bounded by the VPC's width so
	// the cloud never grows wider for the rail alone.
	railPerRow := 1
	if len(rail) > 0 {
		railPerRow = max(1, (vpcW+dgRailGap)/(dgRailW+dgRailGap))
	}
	railRows := 0
	if len(rail) > 0 {
		railRows = (len(rail) + railPerRow - 1) / railPerRow
	}
	railH := 0
	if railRows > 0 {
		railH = dgGapVPCRail + railRows*dgRailH + (railRows-1)*dgRailGap
	}

	contentW := max(vpcW, dgInternetW)
	cloudW := contentW + 2*dgCloudPad
	canvasW := max(cloudW, dgLegendWidth()) + 2*dgMargin
	centerX := dgMargin + cloudW/2

	cloudY := dgMargin
	topY := cloudY + dgCloudHeader
	vpcY := topY
	if hasIGW {
		vpcY = topY + dgInternetH + dgGapNetIGW + dgIGWH + dgGapIGWVPC
	}
	cloudH := (vpcY - cloudY) + vpcH + railH + dgCloudPad
	canvasH := cloudY + cloudH + dgLegendH + dgMargin
	cloudX := centerX - cloudW/2
	vpcX := centerX - vpcW/2

	// ---- layout pass 2: positions ---------------------------------------
	azTop := vpcY + dgVPCHeader + dgVPCPad
	subTop := azTop + dgAZHeader + dgAZPad
	type azBox struct {
		x, y, w, h int
		name       string
	}
	var azBoxes []azBox
	var placed []placedSubnet
	curX := vpcX + dgVPCPad
	for _, az := range azs {
		lanes := azLanes[az]
		azW := lanes*dgSubnetW + (lanes-1)*dgLaneGap + 2*dgAZPad
		azBoxes = append(azBoxes, azBox{x: curX, y: azTop, w: azW, h: colInnerH, name: az})
		gutterX := curX - dgAZGap/2
		if gutterX < vpcX+6 {
			gutterX = vpcX + dgVPCPad/2
		}
		for idx, s := range byAZ[az] {
			lane := idx % lanes
			row := idx / lanes
			ps := placedSubnet{
				info: s, egress: egress[s.ID], enis: eniCount[s.ID], sgs: sgMap[s.ID],
				rt: rtBySubnet[s.ID], nacl: naclBySubnet[s.ID], epIDs: epBySubnet[s.ID],
				x:      curX + dgAZPad + lane*(dgSubnetW+dgLaneGap),
				y:      subTop + row*(dgSubnetH+dgSubnetGap),
				gutter: gutterX,
			}
			if n, ok := natBySubnet[s.ID]; ok {
				nn := n
				ps.nat = &nn
			}
			placed = append(placed, ps)
		}
		curX += azW + dgAZGap
	}
	natByID := map[string]*placedSubnet{}
	for i := range placed {
		if placed[i].nat != nil {
			natByID[placed[i].nat.ID] = &placed[i]
		}
	}

	railTop := vpcY + vpcH + dgGapVPCRail
	for i := range rail {
		row, col := i/railPerRow, i%railPerRow
		inRow := min(railPerRow, len(rail)-row*railPerRow)
		rowW := inRow*dgRailW + (inRow-1)*dgRailGap
		rail[i].x = centerX - rowW/2 + col*(dgRailW+dgRailGap)
		rail[i].y = railTop + row*(dgRailH+dgRailGap)
	}

	// ---- emit -----------------------------------------------------------
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" class="vpcd" fill="#15212B" font-family="system-ui, -apple-system, Segoe UI, Roboto, sans-serif" role="img" aria-label="VPC architecture diagram for %s">`,
		canvasW, canvasH, canvasW, canvasH, esc(data.VPC.ID))
	b.WriteString("\n<defs>\n")
	// One marker per line colour: SVG markers don't inherit the line's stroke,
	// and context-stroke isn't supported widely enough to rely on.
	for _, m := range dgMarkers {
		fmt.Fprintf(&b, `<marker id="%s" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" class="%s"/></marker>`+"\n", m.id, m.class)
	}
	b.WriteString("</defs>\n")
	graph := dgGraph{byRT: map[string][]string{}}
	for i := range placed {
		if rt := placed[i].rt; rt != "" {
			graph.byRT[rt] = append(graph.byRT[rt], placed[i].info.ID)
		}
	}
	graph.writeGraph(&b)
	fmt.Fprintf(&b, `<rect class="canvas" x="0" y="0" width="%d" height="%d" fill="#FFFFFF"/>`+"\n", canvasW, canvasH)

	// AWS Cloud boundary + region.
	b.WriteString("<!-- AWS Cloud -->\n")
	dgRect(&b, "cloud", cloudX, cloudY, cloudW, cloudH, 12)
	cloudLabel := "AWS Cloud"
	if data.VPC.Region != "" {
		cloudLabel += " · " + data.VPC.Region
	}
	dgText(&b, "t-head head-m", cloudX+14, cloudY+18, "start", esc(cloudLabel))

	// VPC container + AZ columns (always visible — structural).
	b.WriteString("<!-- VPC -->\n")
	dgRect(&b, "vpc", vpcX, vpcY, vpcW, vpcH, 8)
	dgRectWH(&b, "vpcband", vpcX+1, vpcY+1, vpcW-2, dgVPCHeader-1, 8)
	vpcLabel := "VPC · " + data.VPC.ID
	if data.VPC.CIDR != "" {
		vpcLabel += "  " + data.VPC.CIDR
	}
	dgText(&b, "t-title", vpcX+14, vpcY+dgVPCHeader/2+6, "start", esc(vpcLabel))
	if len(azs) == 0 {
		dgText(&b, "t-meta", vpcX+vpcW/2, vpcY+dgVPCHeader+40, "middle", "No subnets in this VPC")
	}
	for _, az := range azBoxes {
		b.WriteString("<!-- AZ " + xmlComment(az.name) + " -->\n")
		dgRect(&b, "az", az.x, az.y, az.w, az.h, 6)
		dgRectWH(&b, "azband", az.x+1, az.y+1, az.w-2, dgAZHeader-1, 6)
		dgText(&b, "t-head head-m", az.x+az.w/2, az.y+dgAZHeader/2+5, "middle", esc("Availability Zone · "+az.name))
	}

	// Layer: subnet boxes.
	b.WriteString(`<g data-layer="subnets">` + "\n")
	for i := range placed {
		dgSubnetBox(&b, &placed[i])
	}
	b.WriteString("</g>\n")

	// Layer: detail labels (CIDR / ENIs / egress tag).
	b.WriteString(`<g data-layer="labels">` + "\n")
	for i := range placed {
		dgSubnetLabels(&b, &placed[i])
	}
	b.WriteString("</g>\n")

	// Layer: the route table each subnet is associated with.
	b.WriteString(`<g data-layer="routing">` + "\n")
	for i := range placed {
		if placed[i].rt != "" {
			dgChipLeft(&b, &placed[i], "rt", placed[i].rt, 0, "RT "+placed[i].rt)
		}
	}
	b.WriteString("</g>\n")

	// Layer: network ACLs.
	b.WriteString(`<g data-layer="nacl">` + "\n")
	for i := range placed {
		if placed[i].nacl != "" {
			dgChipRight(&b, &placed[i], "nacl", placed[i].nacl, 0, "ACL "+placed[i].nacl)
		}
	}
	b.WriteString("</g>\n")

	// Layer: security-group badges.
	b.WriteString(`<g data-layer="sg">` + "\n")
	for i := range placed {
		dgSubnetSG(&b, &placed[i])
	}
	b.WriteString("</g>\n")

	// Layer: NAT gateways.
	b.WriteString(`<g data-layer="nat">` + "\n")
	for i := range placed {
		dgSubnetNat(&b, &placed[i])
	}
	b.WriteString("</g>\n")

	// Layer: VPC endpoints — interface endpoints as a chip in each subnet that
	// holds one of their ENIs, gateway endpoints as rail nodes.
	b.WriteString(`<g data-layer="endpoints">` + "\n")
	for i := range placed {
		if len(placed[i].epIDs) > 0 {
			dgChipLeft(&b, &placed[i], "ep", singleID(placed[i].epIDs), 1, endpointChipLabel(placed[i].epIDs))
		}
	}
	dgRailGroup(&b, rail, placed, rtByID, "endpoints", vpcY+vpcH)
	b.WriteString("</g>\n")

	// Layer: peerings, transit gateways and VPN gateways.
	b.WriteString(`<g data-layer="peering">` + "\n")
	dgRailGroup(&b, rail, placed, rtByID, "peering", vpcY+vpcH)
	b.WriteString("</g>\n")

	// Layer: traffic flow (arrows + internet/IGW backbone).
	b.WriteString(`<g data-layer="traffic">` + "\n")
	vpcTopInner := vpcY + dgVPCHeader
	// Egress lines leave a card sideways and climb the channel beside its AZ
	// column. Drawn straight up they would cross every card and AZ heading
	// above them, which is what makes a generated diagram look automatic.
	for i := range placed {
		ps := placed[i]
		switch ps.egress.kind {
		case "igw":
			if hasIGW {
				dgGutterUp(&b, "flow pub", "arrow-pub", dgEdgeAttrs(ps.info.ID, igwID), ps.x, ps.y+22, ps.gutter, vpcTopInner+2)
			}
		case "nat":
			if tn, ok := natByID[ps.egress.target]; ok && tn.info.ID != ps.info.ID {
				// Enter the NAT pill on the side the line arrives from, or it
				// crosses the pill to reach the far edge and strikes out its
				// label.
				pillL, pillR := tn.x+14, tn.x+dgSubnetW-14
				target := pillL
				if ps.gutter > pillR {
					target = pillR
				}
				dgGutterTo(&b, "flow priv", "arrow-priv", dgEdgeAttrs(ps.info.ID, tn.nat.ID),
					ps.x, ps.y+22, ps.gutter, target, tn.y+dgSubnetH-dgChipH-6+dgChipH/2)
			}
		}
	}
	if hasIGW {
		for _, id := range sortedNatIDs(natByID) {
			ps := natByID[id]
			dgGutterUp(&b, "flow edge", "arrow-edge", dgEdgeAttrs(id, igwID),
				ps.x, ps.y+dgSubnetH-dgChipH-6+dgChipH/2, ps.gutter, vpcTopInner+2)
		}
		netX := centerX - dgInternetW/2
		fmt.Fprintf(&b, `<g%s>`+"\n", dgNodeAttrs("internet", "The public internet"))
		dgRect(&b, "node", netX, topY, dgInternetW, dgInternetH, 22)
		dgText(&b, "t-name", centerX, topY+dgInternetH/2+5, "middle", "Internet")
		b.WriteString("</g>\n")
		igwY := topY + dgInternetH + dgGapNetIGW
		igwX := centerX - dgIGWW/2
		dgArrow2(&b, "flow", "arrow", dgEdgeAttrs("internet", igwID), centerX, topY+dgInternetH, centerX, igwY)
		igwLabel := "Internet gateway"
		if igwID != "" {
			igwLabel += " · " + igwID
		}
		fmt.Fprintf(&b, `<g%s>`+"\n", dgNodeAttrs(igwID, igwLabel))
		dgRect(&b, "edge", igwX, igwY, dgIGWW, dgIGWH, 6)
		dgText(&b, "t-chip ct", centerX, igwY+dgIGWH/2+4, "middle", esc(dgTrunc(igwLabel, 30)))
		b.WriteString("</g>\n")
		dgArrow(&b, "flow edge", "arrow-edge", dgEdgeAttrs(igwID, data.VPC.ID), centerX, igwY+dgIGWH, centerX, vpcY)
	}
	b.WriteString("</g>\n")

	dgLegend(&b, dgMargin, cloudY+cloudH+16)
	b.WriteString("</svg>")
	return b.String()
}

// standaloneDiagramSVG is the diagram as its own file. Inline in the report the
// SVG takes its colours from the page's stylesheet; on its own there is no page,
// so the same stylesheet is embedded in the SVG itself — which also carries the
// dark-mode block, so the exported file follows the viewer's system theme the
// way the report does. Embedding the report's own stylesheet, rather than a
// second copy of the diagram's rules, keeps one source of truth for how the
// diagram looks.
func standaloneDiagramSVG(data fullExport) string {
	svg := vpcDiagramSVG(data)
	i := strings.Index(svg, "<defs>")
	if i < 0 {
		return svg
	}
	// CSS is embedded in CDATA: an unescaped ">" or "&" in a selector would
	// otherwise make the file invalid XML.
	style := "<style><![CDATA[\n" + reportCSS + "\n]]></style>\n"
	return svg[:i] + style + svg[i:]
}

// dgNodeAttrs makes an element a first-class node of the diagram: something the
// reader can hover, tab to and click. The id ties it to the relationship graph
// (see dgGraph) and to its row in the tables below; the <title> gives every
// node a plain browser tooltip, which works with no script at all.
func dgNodeAttrs(id, label string) string {
	return fmt.Sprintf(` data-node="%s" tabindex="0" role="img" aria-label="%s"`, esc(id), esc(label))
}

// dgEdgeAttrs tags a line with the two nodes it joins, so hovering either end
// can light up the line and the node at the other end of it.
func dgEdgeAttrs(from, to string) string {
	return fmt.Sprintf(` data-from="%s" data-to="%s"`, esc(from), esc(to))
}

// dgGraph is the relationship graph the page's script reads out of the SVG:
// which subnets share a route table, so hovering a route-table chip can show
// everything it governs. Edges are on the lines themselves.
type dgGraph struct {
	byRT map[string][]string
}

// writeGraph emits the graph as JSON in the SVG's <metadata>, where it travels
// with the diagram instead of being rebuilt by the script from the drawing.
func (g dgGraph) writeGraph(b *strings.Builder) {
	rts := make([]string, 0, len(g.byRT))
	for rt := range g.byRT {
		rts = append(rts, rt)
	}
	sort.Strings(rts)
	b.WriteString(`<metadata id="vpcd-graph">{"rt":{`)
	for i, rt := range rts {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(b, `%q:[`, rt)
		for j, sn := range g.byRT[rt] {
			if j > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, "%q", sn)
		}
		b.WriteString("]")
	}
	b.WriteString("}}</metadata>\n")
}

// dgMarkers is one arrowhead per line colour (see the comment where they are
// emitted).
var dgMarkers = []struct{ id, class string }{
	{"arrow", "flow"},
	{"arrow-pub", "flow pub"},
	{"arrow-priv", "flow priv"},
	{"arrow-edge", "flow edge"},
	{"arrow-peer", "flow peer"},
}

// Each toggleable chip has a fixed slot on the card — two rows of two, left
// and right — rather than being packed in the order they happen to exist.
// Space for all four is reserved whether or not they are drawn, so switching a
// layer off never reflows the card or moves another chip.
const (
	dgChipRow0 = 84
	dgChipRow1 = 106
)

// chipY is the top of one of the two chip rows.
func chipY(ps *placedSubnet, row int) int {
	if row == 0 {
		return ps.y + dgChipRow0
	}
	return ps.y + dgChipRow1
}

// buildRailNodes lists what the VPC connects to outside itself, in a stable
// order: gateway endpoints, then peerings, transit gateway attachments and VPN
// gateways. They are listed from the snapshot, not inferred from routes, so an
// attachment that nothing routes to still appears — drawn without a line, which
// is itself worth seeing.
func buildRailNodes(data fullExport) []railNode {
	var out []railNode
	snap := data.Snap

	var gw []EndpointInfo
	for _, ep := range snap.Endpoints {
		if !strings.EqualFold(ep.Type, "Interface") {
			gw = append(gw, ep)
		}
	}
	sort.Slice(gw, func(i, j int) bool { return gw[i].ID < gw[j].ID })
	for _, ep := range gw {
		out = append(out, railNode{
			id: ep.ID, kind: "endpoint", layer: "endpoints",
			title: "Gateway endpoint", sub: shortServiceName(ep.ServiceName),
		})
	}

	peers := append([]PeeringInfo(nil), snap.Peerings...)
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	for _, p := range peers {
		other := p.AccepterVPCID
		if other == snap.VPCID || other == "" {
			other = p.RequesterVPCID
		}
		out = append(out, railNode{
			id: p.ID, kind: "peering", layer: "peering",
			title: "VPC peering", sub: "to " + other,
		})
	}

	tgws := append([]TransitGatewayAttachmentInfo(nil), data.TransitGatewayAttachments...)
	sort.Slice(tgws, func(i, j int) bool { return tgws[i].ID < tgws[j].ID })
	for _, t := range tgws {
		out = append(out, railNode{
			id: t.TransitGatewayID, kind: "tgw", layer: "peering",
			title: "Transit gateway", sub: t.ID,
		})
	}

	vgws := append([]VPNGatewayInfo(nil), data.VPNGateways...)
	sort.Slice(vgws, func(i, j int) bool { return vgws[i].ID < vgws[j].ID })
	for _, v := range vgws {
		out = append(out, railNode{
			id: v.ID, kind: "vgw", layer: "peering",
			title: "VPN gateway", sub: v.State,
		})
	}
	return out
}

// dgRailGroup draws the rail nodes of one layer and the lines from the subnets
// that route to them.
func dgRailGroup(b *strings.Builder, rail []railNode, placed []placedSubnet, rtByID map[string]RouteTableInfo, layer string, vpcBottom int) {
	for _, n := range rail {
		if n.layer != layer {
			continue
		}
		// Lines first, so a node's box is never drawn over by its own line.
		for i := range placed {
			ps := placed[i]
			if !routesTo(rtByID[ps.rt], n.id) {
				continue
			}
			cx := ps.x + dgSubnetW/2
			dgElbowDown(b, "flow peer", "arrow-peer", dgEdgeAttrs(ps.info.ID, n.id),
				cx, ps.y+dgSubnetH, n.x+dgRailW/2, n.y, vpcBottom)
		}
		class := "node"
		if n.kind == "endpoint" {
			class = "chip ep"
		}
		fmt.Fprintf(b, `<g%s>`+"\n", dgNodeAttrs(n.id, n.title+" "+n.id))
		dgRect(b, class, n.x, n.y, dgRailW, dgRailH, 6)
		dgText(b, "t-chip ct", n.x+12, n.y+21, "start", esc(n.title))
		dgText(b, "t-id", n.x+12, n.y+38, "start", esc(dgTrunc(n.id, 30)))
		if n.sub != "" {
			dgText(b, "t-meta", n.x+12, n.y+52, "start", esc(dgTrunc(n.sub, 32)))
		}
		b.WriteString("</g>\n")
	}
}

// routesTo reports whether a route table has any route pointing at target.
func routesTo(rt RouteTableInfo, target string) bool {
	if target == "" {
		return false
	}
	for _, r := range rt.Routes {
		if r.Target == target {
			return true
		}
	}
	return false
}

// endpointChipLabel names the interface endpoints in a subnet, abbreviating
// past the second so the chip stays inside the card.
func endpointChipLabel(ids []string) string {
	switch {
	case len(ids) == 1:
		return "Endpoint " + ids[0]
	case len(ids) == 2:
		return "Endpoints " + strings.Join(ids, ", ")
	default:
		return fmt.Sprintf("Endpoints %s +%d", strings.Join(ids[:2], ", "), len(ids)-2)
	}
}

// singleID names a chip's node only when it stands for exactly one resource:
// a chip reading "Endpoints a, b +3" is not any one of them.
func singleID(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// shortServiceName trims the "com.amazonaws.<region>." prefix off an endpoint's
// service name, leaving the part that identifies the service ("s3").
func shortServiceName(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

// sortedNatIDs orders the NAT gateways so the emitted arrows are stable.
func sortedNatIDs(m map[string]*placedSubnet) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// subnetAria describes a subnet card for a screen reader (and as its tooltip):
// what it is, how big, and how it reaches the internet — the same three facts
// the card shows visually.
func subnetAria(ps *placedSubnet) string {
	kind := "Isolated subnet"
	switch ps.egress.kind {
	case "igw":
		kind = "Public subnet"
	case "nat":
		kind = "Private subnet"
	}
	out := kind + " " + ps.info.ID
	if ps.info.CIDR != "" {
		out += ", " + ps.info.CIDR
	}
	if ps.info.AZ != "" {
		out += ", in " + ps.info.AZ
	}
	return out + ", " + egressTag(ps.egress)
}

// dgCardTitle is the subnet's display name on the card: the Name tag when it
// has one (the ID is on the line below it either way), the ID otherwise.
func dgCardTitle(s SubnetInfo) string {
	if s.Name != "" && s.Name != "-" {
		return s.Name
	}
	return s.ID
}

// dgSubnetBox draws the subnet's card, accent bar, name and ID.
func dgSubnetBox(b *strings.Builder, ps *placedSubnet) {
	fmt.Fprintf(b, `<g class="%s"%s>`+"\n", dgSubnetClass(ps.egress.kind),
		dgNodeAttrs(ps.info.ID, subnetAria(ps)))
	dgRect(b, "box", ps.x, ps.y, dgSubnetW, dgSubnetH, 6)
	dgText(b, "t-name", ps.x+14, ps.y+24, "start", esc(dgTrunc(dgCardTitle(ps.info), 30)))
	dgText(b, "t-id", ps.x+14, ps.y+41, "start", esc(ps.info.ID))
	b.WriteString("</g>\n")
}

// dgSubnetLabels draws the CIDR/ENI line and the egress tag (detail labels).
func dgSubnetLabels(b *strings.Builder, ps *placedSubnet) {
	cidr := ps.info.CIDR
	if cidr == "" {
		cidr = "—"
	}
	fmt.Fprintf(b, `<g class="%s" data-node="%s">`+"\n", dgSubnetClass(ps.egress.kind), esc(ps.info.ID))
	dgText(b, "t-meta", ps.x+14, ps.y+58, "start", esc(cidr+"  ·  "+plural(ps.enis, "ENI", "ENIs")))
	dgText(b, "t-tag", ps.x+14, ps.y+76, "start", esc(egressTag(ps.egress)))
	b.WriteString("</g>\n")
}

// dgSubnetSG draws a compact security-group badge for the subnet's ENIs.
func dgSubnetSG(b *strings.Builder, ps *placedSubnet) {
	if len(ps.sgs) == 0 {
		return
	}
	label := "SG " + strings.Join(ps.sgs, ", ")
	if len(ps.sgs) > 2 {
		label = fmt.Sprintf("SG %s +%d", strings.Join(ps.sgs[:2], ", "), len(ps.sgs)-2)
	}
	dgChipRight(b, ps, "", "", 1, label)
}

// dgSubnetNat draws the NAT-gateway pill when one lives in the subnet.
func dgSubnetNat(b *strings.Builder, ps *placedSubnet) {
	if ps.nat == nil {
		return
	}
	pillW := dgSubnetW - 28
	pillY := ps.y + dgSubnetH - dgChipH - 6
	fmt.Fprintf(b, `<g%s>`+"\n", dgNodeAttrs(ps.nat.ID, "NAT gateway "+ps.nat.ID))
	dgRect(b, "chip nat", ps.x+14, pillY, pillW, dgChipH, 9)
	dgText(b, "t-chip ct", ps.x+dgSubnetW/2, pillY+dgChipH/2+4, "middle", esc("NAT gateway · "+ps.nat.ID))
	b.WriteString("</g>\n")
}

// ---- small SVG helpers ----------------------------------------------------

// dgSlotChars caps a slot chip's label so two chips always fit side by side on
// a card, whatever the resource is called.
const dgSlotChars = 19

// dgChipLeft draws a chip in the left slot of a chip row. id, when given, makes
// the chip its own node — hovering a route-table chip lights up every subnet
// that table governs.
func dgChipLeft(b *strings.Builder, ps *placedSubnet, kind, id string, row int, label string) {
	dgChipNode(b, kind, id, label, ps.x+14, chipY(ps, row))
}

// dgChipRight draws a chip in the right slot, aligned to the card's right edge
// so its width does not depend on what is in the left slot.
func dgChipRight(b *strings.Builder, ps *placedSubnet, kind, id string, row int, label string) {
	w := dgTextWidth(dgTrunc(label, dgSlotChars), 10.5) + 16
	dgChipNode(b, kind, id, label, ps.x+dgSubnetW-14-w, chipY(ps, row))
}

// dgChipNode draws a slot chip, wrapped as a node when it names a resource.
func dgChipNode(b *strings.Builder, kind, id, label string, x, y int) {
	label = dgTrunc(label, dgSlotChars)
	if id != "" {
		fmt.Fprintf(b, `<g%s>`+"\n", dgNodeAttrs(id, label))
	}
	dgChip(b, kind, x, y, label)
	if id != "" {
		b.WriteString("</g>\n")
	}
}

// dgChip draws a small rounded label chip sized to its text.
func dgChip(b *strings.Builder, kind string, x, y int, label string) {
	label = dgTrunc(label, 34)
	w := dgTextWidth(label, 10.5) + 16
	class := "chip"
	if kind != "" {
		class += " " + kind
	}
	dgRect(b, class, x, y, w, dgChipH, 9)
	dgText(b, "t-chip ct", x+8, y+dgChipH/2+4, "start", esc(label))
}

// dgTextWidth estimates a rendered string's width. The diagram is laid out in
// Go with no access to font metrics, so chips are sized from an average glyph
// width — deliberately generous, since a chip a little too wide reads fine and
// one too narrow clips its label.
func dgTextWidth(s string, size float64) int {
	return int(float64(len([]rune(s)))*size*0.58) + 2
}

func dgRect(b *strings.Builder, class string, x, y, w, h, r int) {
	fmt.Fprintf(b, `<rect class="%s" x="%d" y="%d" width="%d" height="%d" rx="%d"/>`+"\n", class, x, y, w, h, r)
}

// dgRectWH is dgRect for the header bands, which sit inside a rounded border
// and so need their own radius without a stroke.
func dgRectWH(b *strings.Builder, class string, x, y, w, h, r int) {
	dgRect(b, class, x, y, w, h, r)
}

func dgText(b *strings.Builder, class string, x, y int, anchor, content string) {
	fmt.Fprintf(b, `<text class="%s" x="%d" y="%d" text-anchor="%s">%s</text>`+"\n", class, x, y, anchor, content)
}

func dgArrow(b *strings.Builder, class, marker, edge string, x1, y1, x2, y2 int) {
	fmt.Fprintf(b, `<line class="%s"%s x1="%d" y1="%d" x2="%d" y2="%d" marker-end="url(#%s)"/>`+"\n",
		class, edge, x1, y1, x2, y2, marker)
}

func dgArrow2(b *strings.Builder, class, marker, edge string, x1, y1, x2, y2 int) {
	fmt.Fprintf(b, `<line class="%s"%s x1="%d" y1="%d" x2="%d" y2="%d" marker-start="url(#%s)" marker-end="url(#%s)"/>`+"\n",
		class, edge, x1, y1, x2, y2, marker, marker)
}

// dgGutterUp leaves a card by its left edge, crosses into the channel beside
// the AZ column, and climbs it — so the line never runs over a card or a
// heading on its way up.
func dgGutterUp(b *strings.Builder, class, marker, edge string, x1, y1, gutterX, y2 int) {
	fmt.Fprintf(b, `<polyline class="%s"%s points="%d,%d %d,%d %d,%d" marker-end="url(#%s)"/>`+"\n",
		class, edge, x1, y1, gutterX, y1, gutterX, y2, marker)
}

// dgGutterTo is dgGutterUp continued back out of the channel into a target on
// another card (a private subnet reaching its NAT gateway).
func dgGutterTo(b *strings.Builder, class, marker, edge string, x1, y1, gutterX, x2, y2 int) {
	fmt.Fprintf(b, `<polyline class="%s"%s points="%d,%d %d,%d %d,%d %d,%d" marker-end="url(#%s)"/>`+"\n",
		class, edge, x1, y1, gutterX, y1, gutterX, y2, x2, y2, marker)
}

// dgElbowDown routes a line from a subnet's bottom edge, down past the VPC's
// boundary, across, and up into a rail node — so the lines run in the gutter
// between the two instead of over the subnets.
func dgElbowDown(b *strings.Builder, class, marker, edge string, x1, y1, x2, y2, vpcBottom int) {
	midY := vpcBottom + dgGapVPCRail/2
	if midY <= y1 {
		midY = y1 + 4
	}
	fmt.Fprintf(b, `<polyline class="%s"%s points="%d,%d %d,%d %d,%d %d,%d" marker-end="url(#%s)"/>`+"\n",
		class, edge, x1, y1, x1, midY, x2, midY, x2, y2, marker)
}

// dgLegendItems is the key, shared by the width calc and the renderer.
var dgLegendItems = []struct{ class, label string }{
	{"pub", "Public subnet (default route → IGW)"},
	{"priv", "Private subnet (default route → NAT)"},
	{"iso", "Isolated subnet (no default route)"},
	{"edge", "Gateway / NAT"},
	{"peer", "Routes to (dashed)"},
}

func dgLegendItemW(label string) int { return 16 + 6 + dgTextWidth(label, 11.5) + 20 }

func dgLegendWidth() int {
	w := 2 * dgMargin
	for _, it := range dgLegendItems {
		w += dgLegendItemW(it.label)
	}
	return w
}

func dgLegend(b *strings.Builder, x, y int) {
	b.WriteString("<!-- legend -->\n")
	cx := x
	for _, it := range dgLegendItems {
		switch it.class {
		case "peer":
			fmt.Fprintf(b, `<line class="flow peer" x1="%d" y1="%d" x2="%d" y2="%d"/>`+"\n", cx, y+9, cx+16, y+9)
		case "edge":
			dgRect(b, "edge swatch", cx, y, 16, 16, 3)
		default:
			fmt.Fprintf(b, `<g class="%s">`+"\n", it.class)
			dgRect(b, "box swatch", cx, y, 16, 16, 3)
			b.WriteString("</g>\n")
		}
		dgText(b, "t-legend", cx+22, y+13, "start", esc(it.label))
		cx += dgLegendItemW(it.label)
	}
}

// ---- pure data helpers ----------------------------------------------------

// subnetEgress maps each subnet to its default-route (0.0.0.0/0) destination by
// resolving its associated route table (falling back to the VPC's main table).
func subnetEgress(snap vpcSnapshot) map[string]dgEgress {
	rtBySubnet, rtByID := routeTablesBySubnet(snap)
	out := make(map[string]dgEgress, len(snap.Subnets))
	for _, s := range snap.Subnets {
		eg := dgEgress{kind: "none"}
		if rt, ok := rtByID[rtBySubnet[s.ID]]; ok {
			for _, r := range rt.Routes {
				if r.Destination != "0.0.0.0/0" {
					continue
				}
				switch {
				case strings.HasPrefix(r.Target, "igw-"), strings.HasPrefix(r.Target, "eigw-"):
					eg = dgEgress{kind: "igw", target: r.Target}
				case strings.HasPrefix(r.Target, "nat-"):
					eg = dgEgress{kind: "nat", target: r.Target}
				case r.Target != "" && r.Target != "local":
					eg = dgEgress{kind: "other", target: r.Target}
				}
			}
		}
		out[s.ID] = eg
	}
	return out
}

// routeTablesBySubnet resolves which route table governs each subnet — its
// explicit association, or the VPC's main table when it has none, which is what
// AWS actually does — and returns the tables by ID alongside.
func routeTablesBySubnet(snap vpcSnapshot) (bySubnet map[string]string, byID map[string]RouteTableInfo) {
	byID = make(map[string]RouteTableInfo, len(snap.RouteTables))
	main := ""
	for _, rt := range snap.RouteTables {
		byID[rt.ID] = rt
		if rt.IsMain {
			main = rt.ID
		}
	}
	bySubnet = make(map[string]string, len(snap.Subnets))
	for _, s := range snap.Subnets {
		bySubnet[s.ID] = main
	}
	for _, rt := range snap.RouteTables {
		for _, sid := range rt.Associations {
			bySubnet[sid] = rt.ID
		}
	}
	return bySubnet, byID
}

// naclsBySubnet maps each subnet to the network ACL associated with it.
func naclsBySubnet(snap vpcSnapshot) map[string]string {
	out := map[string]string{}
	for _, n := range snap.NetworkACLs {
		for _, sid := range n.Associations {
			out[sid] = n.ID
		}
	}
	return out
}

// interfaceEndpointsBySubnet maps each subnet to the interface endpoints that
// have an ENI in it, sorted for stable output.
func interfaceEndpointsBySubnet(snap vpcSnapshot) map[string][]string {
	out := map[string][]string{}
	for _, ep := range snap.Endpoints {
		if !strings.EqualFold(ep.Type, "Interface") {
			continue
		}
		for _, sid := range ep.SubnetIDs {
			out[sid] = append(out[sid], ep.ID)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// subnetSGs maps each subnet to the sorted, unique security groups used by the
// ENIs in it — the data behind the diagram's toggleable SG layer.
func subnetSGs(snap vpcSnapshot) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, e := range snap.NetworkInterfaces {
		if e.SubnetID == "" {
			continue
		}
		if sets[e.SubnetID] == nil {
			sets[e.SubnetID] = map[string]bool{}
		}
		for _, sg := range e.SecurityGroups {
			if sg != "" {
				sets[e.SubnetID][sg] = true
			}
		}
	}
	out := make(map[string][]string, len(sets))
	for sn, set := range sets {
		ids := make([]string, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out[sn] = ids
	}
	return out
}

func eniCountBySubnet(snap vpcSnapshot) map[string]int {
	out := map[string]int{}
	for _, e := range snap.NetworkInterfaces {
		if e.SubnetID != "" {
			out[e.SubnetID]++
		}
	}
	return out
}

// egressRank orders subnet classes within an AZ column (public on top).
func egressRank(kind string) int {
	switch kind {
	case "igw":
		return 0
	case "nat":
		return 1
	case "other":
		return 2
	default:
		return 3
	}
}

// dgSubnetClass is the CSS class that colours a subnet card by its egress.
func dgSubnetClass(kind string) string {
	switch kind {
	case "igw":
		return "pub"
	case "nat":
		return "priv"
	default:
		return "iso"
	}
}

func egressTag(eg dgEgress) string {
	switch eg.kind {
	case "igw":
		return "→ internet via " + eg.target
	case "nat":
		return "→ " + eg.target
	case "other":
		return "→ " + eg.target
	default:
		return "no default route"
	}
}

// dgTrunc shortens s to n runes with an ellipsis.
func dgTrunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// esc XML-escapes text content for the SVG.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// xmlComment neutralises a string for use inside an XML comment ("--" is illegal).
func xmlComment(s string) string {
	return strings.ReplaceAll(s, "--", "—")
}
