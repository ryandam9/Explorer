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
// / isolated by their default route), NAT gateways drawn in their subnet, and
// arrows for the traffic-flow paths — internet ⇄ IGW, public subnets → IGW,
// private subnets → NAT → IGW. It is a pure function over the export snapshot
// (no AWS calls, no AI), so it is stable for a given input and unit-testable.
//
// The output is a standalone <svg> document (carries its own xmlns), so it both
// embeds inline in the HTML report and writes to a .svg file as-is.
// ---------------------------------------------------------------------------

// Diagram geometry (pixels). Hand-tuned so a typical 2-AZ VPC reads cleanly and
// larger ones grow without overlap.
const (
	dgMargin     = 36
	dgInternetW  = 168
	dgInternetH  = 56
	dgIGWW       = 140
	dgIGWH       = 48
	dgGapNetIGW  = 44 // internet ↕ IGW connector length
	dgGapIGWVPC  = 40 // IGW ↕ VPC top connector length
	dgVPCHeader  = 46
	dgVPCPad     = 26
	dgAZHeader   = 30
	dgAZGap      = 28
	dgSubnetW    = 252
	dgSubnetH    = 98
	dgSubnetGap  = 20
	dgNatH       = 24
	dgLegendH    = 64
	dgRowUnderAZ = 10 // gap between an AZ header and its first subnet
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
)

// dgEgress is a subnet's default-route (0.0.0.0/0) destination, used to classify
// the subnet and to draw its traffic-flow arrow.
type dgEgress struct {
	kind   string // "igw" public · "nat" private · "other" (tgw/pcx/…) · "none" isolated
	target string // the route target id (igw-…, nat-…, tgw-…), for the label
}

// dgSubnet is a laid-out subnet box.
type dgSubnet struct {
	info   SubnetInfo
	egress dgEgress
	enis   int
	nat    *NatGWInfo // non-nil when a NAT gateway lives in this subnet
	x, y   int
	w, h   int
}

// vpcDiagramSVG builds the architecture diagram for the export.
func vpcDiagramSVG(data fullExport) string {
	snap := data.Snap
	egress := subnetEgress(snap)
	eniCount := eniCountBySubnet(snap)

	// NATs by the subnet they live in (to draw the NAT node) and by id (to aim
	// private-subnet arrows at the right NAT).
	natBySubnet := map[string]NatGWInfo{}
	natByID := map[string]*dgSubnet{} // filled after layout
	for i := range snap.NatGateways {
		n := snap.NatGateways[i]
		if n.SubnetID != "" {
			natBySubnet[n.SubnetID] = n
		}
	}

	// Group subnets by AZ; order AZs, and within an AZ order public → private →
	// other → isolated, then by ID, for a stable, readable layout.
	byAZ := map[string][]dgSubnet{}
	for _, s := range snap.Subnets {
		eg := egress[s.ID]
		ds := dgSubnet{info: s, egress: eg, enis: eniCount[s.ID], w: dgSubnetW, h: dgSubnetH}
		if n, ok := natBySubnet[s.ID]; ok {
			nn := n
			ds.nat = &nn
		}
		az := s.AZ
		if az == "" {
			az = "(no AZ)"
		}
		byAZ[az] = append(byAZ[az], ds)
	}
	azs := make([]string, 0, len(byAZ))
	for az := range byAZ {
		azs = append(azs, az)
	}
	sort.Strings(azs)
	for _, az := range azs {
		col := byAZ[az]
		sort.SliceStable(col, func(i, j int) bool {
			ri, rj := egressRank(col[i].egress.kind), egressRank(col[j].egress.kind)
			if ri != rj {
				return ri < rj
			}
			return col[i].info.ID < col[j].info.ID
		})
		byAZ[az] = col
	}

	hasIGW := len(snap.InternetGateways) > 0

	// ---- layout ----------------------------------------------------------
	numAZ := len(azs)
	colInnerH := 0
	for _, az := range azs {
		n := len(byAZ[az])
		h := dgAZHeader + dgRowUnderAZ
		if n > 0 {
			h += n*dgSubnetH + (n-1)*dgSubnetGap
		}
		if h > colInnerH {
			colInnerH = h
		}
	}
	if colInnerH == 0 {
		colInnerH = dgAZHeader + dgRowUnderAZ + 40 // "no subnets" note
	}
	vpcInnerW := dgSubnetW
	if numAZ > 0 {
		vpcInnerW = numAZ*dgSubnetW + (numAZ-1)*dgAZGap
	}
	vpcW := vpcInnerW + 2*dgVPCPad
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

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="Roboto Condensed, ui-sans-serif, system-ui, Arial, sans-serif" role="img" aria-label="VPC architecture diagram for %s">`,
		canvasW, canvasH, canvasW, canvasH, esc(data.VPC.ID))
	b.WriteString("\n<defs>\n")
	// A single ink arrowhead; lines carry their own colour, heads stay black.
	b.WriteString(`<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" fill="` + dgInk + `"/></marker>` + "\n")
	b.WriteString("</defs>\n")
	// Background.
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%d" height="%d" fill="#fff8e7"/>`+"\n", canvasW, canvasH)

	// ---- VPC container ---------------------------------------------------
	b.WriteString("<!-- VPC container -->\n")
	dgRect(&b, vpcX, vpcY, vpcW, vpcH, 10, dgVPCFill, dgInk, 4)
	dgRect(&b, vpcX, vpcY, vpcW, dgVPCHeader, 10, dgVPCBand, dgInk, 4)
	vpcLabel := "VPC · " + data.VPC.ID
	if data.VPC.CIDR != "" {
		vpcLabel += "  " + data.VPC.CIDR
	}
	dgText(&b, vpcX+16, vpcY+dgVPCHeader/2+5, 16, "700", "start", dgInk, esc(vpcLabel))

	// Column x positions.
	colX := func(i int) int { return vpcX + dgVPCPad + i*(dgSubnetW+dgAZGap) }
	subTop := vpcY + dgVPCHeader + dgVPCPad + dgAZHeader + dgRowUnderAZ

	if numAZ == 0 {
		dgText(&b, vpcX+vpcW/2, vpcY+dgVPCHeader+40, 14, "400", "middle", dgIsoLine, "No subnets in this VPC")
	}

	// ---- AZ headers + subnet boxes --------------------------------------
	for i, az := range azs {
		x := colX(i)
		b.WriteString("<!-- AZ " + xmlComment(az) + " -->\n")
		dgRect(&b, x, vpcY+dgVPCHeader+dgVPCPad, dgSubnetW, dgAZHeader, 6, dgAZBand, dgInk, 2)
		dgText(&b, x+dgSubnetW/2, vpcY+dgVPCHeader+dgVPCPad+dgAZHeader/2+5, 13, "700", "middle", dgInk, esc(az))

		for j := range byAZ[az] {
			ds := &byAZ[az][j]
			ds.x = x
			ds.y = subTop + j*(dgSubnetH+dgSubnetGap)
			if ds.nat != nil {
				natByID[ds.nat.ID] = ds
			}
			dgDrawSubnet(&b, ds)
		}
	}

	// ---- traffic-flow arrows (drawn on top so heads stay visible) -------
	b.WriteString("<!-- traffic-flow arrows -->\n")
	vpcTopInner := vpcY + dgVPCHeader
	for _, az := range azs {
		for j := range byAZ[az] {
			ds := byAZ[az][j]
			cx := ds.x + ds.w/2
			switch ds.egress.kind {
			case "igw":
				// Public subnet egresses to the internet via the IGW: arrow up to
				// the VPC's top edge (where the IGW connects).
				if hasIGW {
					dgArrow(&b, cx, ds.y, cx, vpcTopInner+2, dgPubLine, false)
				}
			case "nat":
				// Private subnet → its NAT gateway (orthogonal connector).
				if tn, ok := natByID[ds.egress.target]; ok {
					dgElbow(&b, cx, ds.y, tn.x+tn.w/2, tn.y+tn.h-dgNatH, dgPrivLine, true)
				}
			}
		}
	}
	// NAT gateways egress to the IGW: arrow up to the VPC top edge.
	if hasIGW {
		for _, ds := range natByID {
			nx := ds.x + ds.w/2
			dgArrow(&b, nx, ds.y+ds.h-dgNatH, nx, vpcTopInner+2, dgNatLine, false)
		}
	}

	// ---- internet + IGW backbone ----------------------------------------
	if hasIGW {
		b.WriteString("<!-- internet & gateway -->\n")
		netX := centerX - dgInternetW/2
		dgRect(&b, netX, topY, dgInternetW, dgInternetH, 10, dgInternet, dgInk, 4)
		dgText(&b, centerX, topY+dgInternetH/2+6, 18, "700", "middle", dgInk, "INTERNET")

		igwY := topY + dgInternetH + dgGapNetIGW
		igwX := centerX - dgIGWW/2
		// Internet ⇄ IGW (bidirectional traffic).
		dgArrow2(&b, centerX, topY+dgInternetH, centerX, igwY, dgInk)
		dgRect(&b, igwX, igwY, dgIGWW, dgIGWH, 8, dgIGWFill, dgInk, 4)
		igwLabel := "IGW"
		if id := snap.InternetGateways[0].ID; id != "" {
			igwLabel = "IGW · " + id
		}
		dgText(&b, centerX, igwY+dgIGWH/2+5, 13, "700", "middle", dgInk, esc(igwLabel))
		// IGW → VPC top edge.
		dgArrow(&b, centerX, igwY+dgIGWH, centerX, vpcY, dgInk, false)
	}

	// ---- legend ----------------------------------------------------------
	dgLegend(&b, dgMargin, vpcY+vpcH+18)

	b.WriteString("</svg>")
	return b.String()
}

// dgDrawSubnet emits one subnet box: a coloured fill by egress class, the
// subnet name/ID, CIDR, ENI count and egress tag, plus a NAT pill when a NAT
// gateway lives in it.
func dgDrawSubnet(b *strings.Builder, ds *dgSubnet) {
	fill, line := dgSubnetColors(ds.egress.kind)
	dgRect(b, ds.x, ds.y, ds.w, ds.h, 6, fill, dgInk, 3)
	// Left accent bar in the class colour.
	fmt.Fprintf(b, `<rect x="%d" y="%d" width="6" height="%d" fill="%s"/>`+"\n", ds.x+3, ds.y+3, ds.h-6, line)

	name := ds.info.Name
	if name == "" {
		name = ds.info.ID
	}
	dgText(b, ds.x+18, ds.y+24, 14, "700", "start", dgInk, esc(dgTrunc(name, 28)))
	dgText(b, ds.x+18, ds.y+44, 12, "400", "start", dgInk, esc(ds.info.ID))
	cidr := ds.info.CIDR
	if cidr == "" {
		cidr = "—"
	}
	dgText(b, ds.x+18, ds.y+62, 12, "400", "start", dgInk, esc(cidr+"  ·  "+plural(ds.enis, "ENI", "ENIs")))
	dgText(b, ds.x+18, ds.y+80, 11, "700", "start", line, esc(egressTag(ds.egress)))

	if ds.nat != nil {
		pillW := ds.w - 24
		pillY := ds.y + ds.h - dgNatH - 6
		dgRect(b, ds.x+12, pillY, pillW, dgNatH, 4, dgNatFill, dgNatLine, 2)
		dgText(b, ds.x+ds.w/2, pillY+dgNatH/2+4, 11, "700", "middle", dgInk, esc("NAT · "+ds.nat.ID))
	}
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

// dgArrow draws a straight connector with an arrowhead at the end.
func dgArrow(b *strings.Builder, x1, y1, x2, y2 int, stroke string, dashed bool) {
	dash := ""
	if dashed {
		dash = ` stroke-dasharray="5 4"`
	}
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2"%s marker-end="url(#arrow)"/>`+"\n",
		x1, y1, x2, y2, stroke, dash)
}

// dgArrow2 draws a straight connector with arrowheads at both ends.
func dgArrow2(b *strings.Builder, x1, y1, x2, y2 int, stroke string) {
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="%s" stroke-width="2.5" marker-start="url(#arrow)" marker-end="url(#arrow)"/>`+"\n",
		x1, y1, x2, y2, stroke)
}

// dgElbow draws an orthogonal (up-then-across-then-to-target) connector with an
// arrowhead at the end — used for private-subnet → NAT arrows that may span
// columns.
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

// dgLegendItems is the colour key, shared by the width calc and the renderer so
// the canvas is always wide enough to hold the legend.
var dgLegendItems = []struct{ fill, line, label string }{
	{dgPubFill, dgPubLine, "Public subnet (→ IGW)"},
	{dgPrivFill, dgPrivLine, "Private subnet (→ NAT)"},
	{dgIsoFill, dgIsoLine, "Isolated subnet (no default route)"},
	{dgNatFill, dgNatLine, "NAT gateway"},
}

// dgLegendItemW estimates an item's footprint (swatch + gap + label + trailing
// gap) at the legend's 12px font.
func dgLegendItemW(label string) int { return 18 + 6 + len([]rune(label))*7 + 22 }

// dgLegendWidth is the total legend footprint, so the canvas can fit it.
func dgLegendWidth() int {
	w := 0
	for _, it := range dgLegendItems {
		w += dgLegendItemW(it.label)
	}
	return w
}

// dgLegend draws the colour key.
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
