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
// of a VPC: the internet and its gateway at the top, the VPC as a container,
// availability-zone columns of subnets inside it (colour-coded public / private
// / isolated by their default route), NAT gateways drawn in their subnet,
// security-group badges (from each subnet's ENIs), and arrows for the
// traffic-flow paths — internet ⇄ IGW, public subnets → IGW, private subnets →
// NAT → IGW. It is a pure function over the export snapshot (no AWS calls, no
// AI), so it is stable for a given input and unit-testable.
//
// Toggleable parts are wrapped in <g data-layer="…"> groups (subnet, labels,
// sg, nat, traffic) so the HTML report's checkbox bar can show/hide each layer
// with a few lines of inline JS — no external library. The output is a
// standalone <svg> (carries its own xmlns), so it embeds inline in the HTML
// report and writes to a .svg file as-is.
//
// Tall AZ columns wrap into lanes (dgMaxRows per lane) so a 40-subnet VPC stays
// a readable grid instead of one extreme ribbon.
// ---------------------------------------------------------------------------

// Diagram geometry (pixels).
const (
	dgMargin     = 36
	dgInternetW  = 168
	dgInternetH  = 56
	dgIGWW       = 140
	dgIGWH       = 48
	dgGapNetIGW  = 44
	dgGapIGWVPC  = 40
	dgVPCHeader  = 46
	dgVPCPad     = 26
	dgAZHeader   = 30
	dgAZGap      = 28
	dgLaneGap    = 16
	dgSubnetW    = 252
	dgSubnetH    = 124
	dgSubnetGap  = 20
	dgNatH       = 22
	dgLegendH    = 64
	dgRowUnderAZ = 10
	dgMaxRows    = 8 // subnets per lane before an AZ column wraps into another lane
)

// dgPalette — the report's neo-brutalist colours, reused so the diagram matches
// the surrounding HTML.
const (
	dgInk      = "#111111"
	dgInternet = "#00d4ff"
	dgIGWFill  = "#ffe500"
	dgVPCFill  = "#ffffff"
	dgVPCBand  = "#b8ff3c"
	dgAZBand   = "#fff7d6"
	dgPubFill  = "#d6ffd1"
	dgPubLine  = "#1f8a3b"
	dgPrivFill = "#e3efff"
	dgPrivLine = "#1f6feb"
	dgIsoFill  = "#f0f0f0"
	dgIsoLine  = "#777777"
	dgNatFill  = "#ffd6a5"
	dgNatLine  = "#d97706"
	dgSGFill   = "#eeeeee"
	dgSGLine   = "#999999"
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
	x, y   int
}

// vpcDiagramSVG builds the architecture diagram for the export.
func vpcDiagramSVG(data fullExport) string {
	snap := data.Snap
	egress := subnetEgress(snap)
	eniCount := eniCountBySubnet(snap)
	sgMap := subnetSGs(snap)

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
		azW := lanes*dgSubnetW + (lanes-1)*dgLaneGap
		if i > 0 {
			totalInnerW += dgAZGap
		}
		totalInnerW += azW
	}
	if len(azs) == 0 {
		totalInnerW = dgSubnetW
	}
	colInnerH := dgAZHeader + dgRowUnderAZ
	if maxRows > 0 {
		colInnerH += maxRows*dgSubnetH + (maxRows-1)*dgSubnetGap
	} else {
		colInnerH += 40
	}

	vpcW := totalInnerW + 2*dgVPCPad
	vpcH := dgVPCHeader + dgVPCPad + colInnerH + dgVPCPad

	contentW := vpcW
	for _, w := range []int{dgInternetW, dgLegendWidth()} {
		if w > contentW {
			contentW = w
		}
	}
	canvasW := contentW + 2*dgMargin
	centerX := dgMargin + contentW/2

	topY := dgMargin
	vpcY := topY
	if hasIGW {
		vpcY = topY + dgInternetH + dgGapNetIGW + dgIGWH + dgGapIGWVPC
	}
	canvasH := vpcY + vpcH + dgLegendH + dgMargin
	vpcX := centerX - vpcW/2

	// ---- layout pass 2: positions ---------------------------------------
	subTop := vpcY + dgVPCHeader + dgVPCPad + dgAZHeader + dgRowUnderAZ
	type azHdr struct {
		x, w int
		name string
	}
	var headers []azHdr
	var placed []placedSubnet
	curX := vpcX + dgVPCPad
	for _, az := range azs {
		lanes := azLanes[az]
		azW := lanes*dgSubnetW + (lanes-1)*dgLaneGap
		headers = append(headers, azHdr{x: curX, w: azW, name: az})
		for idx, s := range byAZ[az] {
			lane := idx % lanes
			row := idx / lanes
			ps := placedSubnet{
				info: s, egress: egress[s.ID], enis: eniCount[s.ID], sgs: sgMap[s.ID],
				x: curX + lane*(dgSubnetW+dgLaneGap),
				y: subTop + row*(dgSubnetH+dgSubnetGap),
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

	// ---- emit -----------------------------------------------------------
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="Roboto Condensed, ui-sans-serif, system-ui, Arial, sans-serif" role="img" aria-label="VPC architecture diagram for %s">`,
		canvasW, canvasH, canvasW, canvasH, esc(data.VPC.ID))
	b.WriteString("\n<defs>\n")
	b.WriteString(`<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="` + dgInk + `"/></marker>` + "\n")
	b.WriteString("</defs>\n")
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%d" height="%d" fill="#fff8e7"/>`+"\n", canvasW, canvasH)

	// VPC container + AZ headers (always visible — structural).
	b.WriteString("<!-- VPC container -->\n")
	dgRect(&b, vpcX, vpcY, vpcW, vpcH, 10, dgVPCFill, dgInk, 4)
	dgRect(&b, vpcX, vpcY, vpcW, dgVPCHeader, 10, dgVPCBand, dgInk, 4)
	vpcLabel := "VPC · " + data.VPC.ID
	if data.VPC.CIDR != "" {
		vpcLabel += "  " + data.VPC.CIDR
	}
	dgText(&b, vpcX+16, vpcY+dgVPCHeader/2+5, 16, "700", "start", dgInk, esc(vpcLabel))
	if len(azs) == 0 {
		dgText(&b, vpcX+vpcW/2, vpcY+dgVPCHeader+40, 14, "400", "middle", dgIsoLine, "No subnets in this VPC")
	}
	headerY := vpcY + dgVPCHeader + dgVPCPad
	for _, hd := range headers {
		b.WriteString("<!-- AZ " + xmlComment(hd.name) + " -->\n")
		dgRect(&b, hd.x, headerY, hd.w, dgAZHeader, 6, dgAZBand, dgInk, 2)
		dgText(&b, hd.x+hd.w/2, headerY+dgAZHeader/2+5, 13, "700", "middle", dgInk, esc(hd.name))
	}

	// Layer: subnet boxes.
	b.WriteString(`<g data-layer="subnet">` + "\n")
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

	// Layer: traffic flow (arrows + internet/IGW backbone).
	b.WriteString(`<g data-layer="traffic">` + "\n")
	vpcTopInner := vpcY + dgVPCHeader
	for i := range placed {
		ps := placed[i]
		cx := ps.x + dgSubnetW/2
		switch ps.egress.kind {
		case "igw":
			if hasIGW {
				dgArrow(&b, cx, ps.y, cx, vpcTopInner+2, dgPubLine, false)
			}
		case "nat":
			if tn, ok := natByID[ps.egress.target]; ok {
				dgElbow(&b, cx, ps.y, tn.x+dgSubnetW/2, tn.y+dgSubnetH-dgNatH, dgPrivLine, true)
			}
		}
	}
	if hasIGW {
		for _, ps := range natByID {
			nx := ps.x + dgSubnetW/2
			dgArrow(&b, nx, ps.y+dgSubnetH-dgNatH, nx, vpcTopInner+2, dgNatLine, false)
		}
		netX := centerX - dgInternetW/2
		dgRect(&b, netX, topY, dgInternetW, dgInternetH, 10, dgInternet, dgInk, 4)
		dgText(&b, centerX, topY+dgInternetH/2+6, 18, "700", "middle", dgInk, "INTERNET")
		igwY := topY + dgInternetH + dgGapNetIGW
		igwX := centerX - dgIGWW/2
		dgArrow2(&b, centerX, topY+dgInternetH, centerX, igwY, dgInk)
		dgRect(&b, igwX, igwY, dgIGWW, dgIGWH, 8, dgIGWFill, dgInk, 4)
		igwLabel := "IGW"
		if id := snap.InternetGateways[0].ID; id != "" {
			igwLabel = "IGW · " + id
		}
		dgText(&b, centerX, igwY+dgIGWH/2+5, 13, "700", "middle", dgInk, esc(igwLabel))
		dgArrow(&b, centerX, igwY+dgIGWH, centerX, vpcY, dgInk, false)
	}
	b.WriteString("</g>\n")

	dgLegend(&b, dgMargin, vpcY+vpcH+18)
	b.WriteString("</svg>")
	return b.String()
}

// dgSubnetBox draws the subnet's coloured box, accent bar, name and ID.
func dgSubnetBox(b *strings.Builder, ps *placedSubnet) {
	fill, line := dgSubnetColors(ps.egress.kind)
	dgRect(b, ps.x, ps.y, dgSubnetW, dgSubnetH, 6, fill, dgInk, 3)
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="6" height="%d" fill="%s"/>`+"\n", ps.x+3, ps.y+3, dgSubnetH-6, line)
	name := ps.info.Name
	if name == "" {
		name = ps.info.ID
	}
	dgText(b, ps.x+18, ps.y+22, 14, "700", "start", dgInk, esc(dgTrunc(name, 28)))
	dgText(b, ps.x+18, ps.y+39, 12, "400", "start", dgInk, esc(ps.info.ID))
}

// dgSubnetLabels draws the CIDR/ENI line and the egress tag (detail labels).
func dgSubnetLabels(b *strings.Builder, ps *placedSubnet) {
	_, line := dgSubnetColors(ps.egress.kind)
	cidr := ps.info.CIDR
	if cidr == "" {
		cidr = "—"
	}
	dgText(b, ps.x+18, ps.y+57, 12, "400", "start", dgInk, esc(cidr+"  ·  "+plural(ps.enis, "ENI", "ENIs")))
	dgText(b, ps.x+18, ps.y+74, 11, "700", "start", line, esc(egressTag(ps.egress)))
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
	dgText(b, ps.x+18, ps.y+91, 10, "400", "start", dgSGLine, esc(dgTrunc(label, 34)))
}

// dgSubnetNat draws the NAT-gateway pill when one lives in the subnet.
func dgSubnetNat(b *strings.Builder, ps *placedSubnet) {
	if ps.nat == nil {
		return
	}
	pillW := dgSubnetW - 24
	pillY := ps.y + dgSubnetH - dgNatH - 4
	dgRect(b, ps.x+12, pillY, pillW, dgNatH, 4, dgNatFill, dgNatLine, 2)
	dgText(b, ps.x+dgSubnetW/2, pillY+dgNatH/2+4, 11, "700", "middle", dgInk, esc("NAT · "+ps.nat.ID))
}

// ---- small SVG helpers ----------------------------------------------------

func dgRect(b *strings.Builder, x, y, w, h, r int, fill, stroke string, sw int) {
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="%d" height="%d" rx="%d" fill="%s" stroke="%s" stroke-width="%d"/>`+"\n",
		x, y, w, h, r, fill, stroke, sw)
}

func dgText(b *strings.Builder, x, y, size int, weight, anchor, fill, content string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="%d" font-weight="%s" text-anchor="%s" fill="%s">%s</text>`+"\n",
		x, y, size, weight, anchor, fill, content)
}

func dgArrow(b *strings.Builder, x1, y1, x2, y2 int, stroke string, dashed bool) {
	dash := ""
	if dashed {
		dash = ` stroke-dasharray="5 4"`
	}
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2"%s marker-end="url(#arrow)"/>`+"\n",
		x1, y1, x2, y2, stroke, dash)
}

func dgArrow2(b *strings.Builder, x1, y1, x2, y2 int, stroke string) {
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2.5" marker-start="url(#arrow)" marker-end="url(#arrow)"/>`+"\n",
		x1, y1, x2, y2, stroke)
}

func dgElbow(b *strings.Builder, x1, y1, x2, y2 int, stroke string, dashed bool) {
	midY := y1 - dgSubnetGap/2
	if midY < y2 {
		midY = y2
	}
	dash := ""
	if dashed {
		dash = ` stroke-dasharray="5 4"`
	}
	fmt.Fprintf(b, `<polyline points="%d,%d %d,%d %d,%d %d,%d" fill="none" stroke="%s" stroke-width="2"%s marker-end="url(#arrow)"/>`+"\n",
		x1, y1, x1, midY, x2, midY, x2, y2, stroke, dash)
}

// dgLegendItems is the colour key, shared by the width calc and the renderer.
var dgLegendItems = []struct{ fill, line, label string }{
	{dgPubFill, dgPubLine, "Public subnet (→ IGW)"},
	{dgPrivFill, dgPrivLine, "Private subnet (→ NAT)"},
	{dgIsoFill, dgIsoLine, "Isolated subnet (no default route)"},
	{dgNatFill, dgNatLine, "NAT gateway"},
}

func dgLegendItemW(label string) int { return 18 + 6 + len([]rune(label))*7 + 22 }

func dgLegendWidth() int {
	w := 0
	for _, it := range dgLegendItems {
		w += dgLegendItemW(it.label)
	}
	return w
}

func dgLegend(b *strings.Builder, x, y int) {
	b.WriteString("<!-- legend -->\n")
	cx := x
	for _, it := range dgLegendItems {
		dgRect(b, cx, y, 18, 18, 3, it.fill, it.line, 2)
		dgText(b, cx+24, y+14, 12, "400", "start", dgInk, esc(it.label))
		cx += dgLegendItemW(it.label)
	}
}

// ---- pure data helpers ----------------------------------------------------

// subnetEgress maps each subnet to its default-route (0.0.0.0/0) destination by
// resolving its associated route table (falling back to the VPC's main table).
func subnetEgress(snap vpcSnapshot) map[string]dgEgress {
	rtBySubnet := map[string]RouteTableInfo{}
	var mainRT *RouteTableInfo
	for i := range snap.RouteTables {
		rt := snap.RouteTables[i]
		if rt.IsMain {
			mainRT = &snap.RouteTables[i]
		}
		for _, sid := range rt.Associations {
			rtBySubnet[sid] = rt
		}
	}
	out := make(map[string]dgEgress, len(snap.Subnets))
	for _, s := range snap.Subnets {
		rt, ok := rtBySubnet[s.ID]
		if !ok && mainRT != nil {
			rt, ok = *mainRT, true
		}
		eg := dgEgress{kind: "none"}
		if ok {
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

func dgSubnetColors(kind string) (fill, line string) {
	switch kind {
	case "igw":
		return dgPubFill, dgPubLine
	case "nat":
		return dgPrivFill, dgPrivLine
	default:
		return dgIsoFill, dgIsoLine
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
