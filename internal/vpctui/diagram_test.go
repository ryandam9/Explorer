package vpctui

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

// diagramFixture is a two-AZ VPC with an IGW, a public and a private subnet per
// AZ, and a NAT gateway, exercising every branch of the diagram layout.
func diagramFixture() fullExport {
	return fullExport{
		VPC: VPCInfo{ID: "vpc-1", CIDR: "10.0.0.0/16", Region: "ap-southeast-2"},
		Snap: vpcSnapshot{
			VPCID:            "vpc-1",
			InternetGateways: []IGWInfo{{ID: "igw-1", State: "available"}},
			Subnets: []SubnetInfo{
				{ID: "subnet-pub-a", CIDR: "10.0.0.0/24", AZ: "ap-southeast-2a"},
				{ID: "subnet-priv-a", CIDR: "10.0.1.0/24", AZ: "ap-southeast-2a"},
				{ID: "subnet-pub-b", CIDR: "10.0.2.0/24", AZ: "ap-southeast-2b"},
				{ID: "subnet-priv-b", CIDR: "10.0.3.0/24", AZ: "ap-southeast-2b"},
			},
			RouteTables: []RouteTableInfo{
				{ID: "rtb-pub", Associations: []string{"subnet-pub-a", "subnet-pub-b"},
					Routes: []Route{{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"}}},
				{ID: "rtb-priv", IsMain: true, Associations: []string{"subnet-priv-a", "subnet-priv-b"},
					Routes: []Route{{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"}}},
			},
			NatGateways: []NatGWInfo{{ID: "nat-1", SubnetID: "subnet-pub-a", State: "available"}},
			NetworkInterfaces: []ENIInfo{
				{ID: "eni-1", SubnetID: "subnet-priv-a", SecurityGroups: []string{"sg-app", "sg-db"}},
				{ID: "eni-2", SubnetID: "subnet-priv-a", SecurityGroups: []string{"sg-app"}},
			},
		},
	}
}

func TestVPCDiagramSVGStructure(t *testing.T) {
	svg := vpcDiagramSVG(diagramFixture())
	for _, want := range []string{
		"<svg",
		"viewBox=",
		`class="vpcd"`,
		`aria-label="VPC architecture diagram for vpc-1"`,
		`<marker id="arrow"`,
		"AWS Cloud · ap-southeast-2",          // cloud boundary carries the region
		"VPC · vpc-1",                         // VPC container label
		"Internet",                            // internet node
		"Internet gateway · igw-1",            // gateway node
		"Availability Zone · ap-southeast-2a", // AZ containers
		"Availability Zone · ap-southeast-2b",
		"subnet-pub-a", // subnet cards
		"subnet-priv-b",
		"NAT gateway · nat-1",  // NAT pill in its subnet
		"→ internet via igw-1", // public-subnet egress tag
		"→ nat-1",              // private-subnet egress tag
		"2 ENIs",               // ENI count on subnet-priv-a
		"RT rtb-pub",           // the route table governing each subnet
		"RT rtb-priv",
		"Public subnet (default route → IGW)", // legend
		"Private subnet (default route → NAT)",
		"Routes to (dashed)",
		// Toggleable layer groups.
		`<g data-layer="subnets">`,
		`<g data-layer="labels">`,
		`<g data-layer="sg">`,
		`<g data-layer="nat">`,
		`<g data-layer="routing">`,
		`<g data-layer="nacl">`,
		`<g data-layer="endpoints">`,
		`<g data-layer="peering">`,
		`<g data-layer="traffic">`,
		"SG sg-app", // security-group badge from the subnet's ENIs
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("diagram SVG missing %q", want)
		}
	}

	// Colours live in the report's stylesheet, not in the markup: a diagram
	// carrying its own fills could never follow light/dark mode. Two neutral
	// fallbacks on the root and the canvas are deliberate — they are all a
	// standalone .svg, opened with no stylesheet at all, has to go on — so the
	// scan starts after them and every shape past that point must be classed.
	body := svg[strings.Index(svg, "<!-- AWS Cloud -->"):]
	for _, attr := range []string{"fill=", "stroke=", "stroke-dasharray=", "stroke-width="} {
		if i := strings.Index(body, attr); i >= 0 {
			t.Errorf("diagram hard-codes styling instead of using a class: %q",
				body[i:min(i+70, len(body))])
		}
	}
}

// The main route table governs a subnet with no association of its own — the
// same rule AWS applies, and what decides whether a subnet reads as isolated.
func TestRouteTablesBySubnetFallsBackToMain(t *testing.T) {
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{{ID: "s-assoc"}, {ID: "s-bare"}},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-main", IsMain: true},
			{ID: "rtb-x", Associations: []string{"s-assoc"}},
		},
	}
	bySubnet, byID := routeTablesBySubnet(snap)
	if bySubnet["s-assoc"] != "rtb-x" {
		t.Errorf("associated subnet → %q, want rtb-x", bySubnet["s-assoc"])
	}
	if bySubnet["s-bare"] != "rtb-main" {
		t.Errorf("unassociated subnet → %q, want the main table", bySubnet["s-bare"])
	}
	if _, ok := byID["rtb-main"]; !ok {
		t.Error("route tables should be returned by ID as well")
	}
}

// relationFixture adds the things a VPC connects to: a gateway endpoint, an
// interface endpoint, a peering, a transit gateway and a network ACL.
func relationFixture() fullExport {
	f := diagramFixture()
	f.Snap.Endpoints = []EndpointInfo{
		{ID: "vpce-gw", Type: "Gateway", ServiceName: "com.amazonaws.ap-southeast-2.s3",
			RouteTableIDs: []string{"rtb-priv"}},
		{ID: "vpce-if", Type: "Interface", ServiceName: "com.amazonaws.ap-southeast-2.ssm",
			SubnetIDs: []string{"subnet-priv-a"}},
	}
	f.Snap.Peerings = []PeeringInfo{{ID: "pcx-1", RequesterVPCID: "vpc-1", AccepterVPCID: "vpc-2"}}
	f.Snap.NetworkACLs = []NACLInfo{{ID: "acl-1", Associations: []string{"subnet-pub-a"}}}
	f.TransitGatewayAttachments = []TransitGatewayAttachmentInfo{
		{ID: "tgw-attach-1", TransitGatewayID: "tgw-1"},
	}
	// The private table routes to the peering, the gateway endpoint and the
	// transit gateway, so those relationships have lines to draw.
	for i := range f.Snap.RouteTables {
		if f.Snap.RouteTables[i].ID == "rtb-priv" {
			f.Snap.RouteTables[i].Routes = append(f.Snap.RouteTables[i].Routes,
				Route{Destination: "10.1.0.0/16", Target: "pcx-1"},
				Route{Destination: "pl-6ca54005", Target: "vpce-gw"},
				Route{Destination: "10.9.0.0/16", Target: "tgw-1"})
		}
	}
	return f
}

// Every relationship in the snapshot is on the diagram: what a subnet is bound
// to (route table, ACL, interface endpoint) on its card, and what the VPC
// reaches (peering, transit gateway, gateway endpoint) on the edge rail.
func TestVPCDiagramSVGDrawsRelationships(t *testing.T) {
	svg := vpcDiagramSVG(relationFixture())
	for _, want := range []string{
		"ACL acl-1",        // the network ACL on the subnet it governs
		"Endpoint vpce-if", // the interface endpoint in the subnet holding its ENI
		"Gateway endpoint", // the gateway endpoint as a rail node
		"vpce-gw",
		"VPC peering",
		"pcx-1",
		"to vpc-2", // the peering names the other side
		"Transit gateway",
		"tgw-1",
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("diagram SVG missing %q", want)
		}
	}
	// A line per (subnet, rail node) pair the route table actually routes to:
	// the two private subnets share rtb-priv, which routes to all three nodes.
	// Counted by the arrowhead, which only a real relationship line carries —
	// the legend's swatch uses the same class but no marker.
	if got := strings.Count(svg, `marker-end="url(#arrow-peer)"`); got < 6 {
		t.Errorf("got %d routes-to lines, want one per routed subnet/target pair (≥6)", got)
	}
	assertSVGWithinViewBox(t, svg)
}

// A rail node nothing routes to is still drawn — "attached but unrouted" is a
// fact worth seeing, not a reason to hide it.
func TestVPCDiagramSVGShowsUnroutedAttachment(t *testing.T) {
	f := diagramFixture()
	f.Snap.Peerings = []PeeringInfo{{ID: "pcx-orphan", RequesterVPCID: "vpc-1", AccepterVPCID: "vpc-9"}}
	svg := vpcDiagramSVG(f)
	if !strings.Contains(svg, "pcx-orphan") {
		t.Error("a peering with no route should still appear on the diagram")
	}
	if strings.Contains(svg, `marker-end="url(#arrow-peer)"`) {
		t.Error("a peering with no route should have no line drawn to it")
	}
}

// TestVPCDiagramSVGWellFormed parses the output to ensure it is valid XML (a
// single well-formed <svg> tree) — a junk diagram that doesn't parse is worse
// than none.
func TestVPCDiagramSVGWellFormed(t *testing.T) {
	for name, data := range map[string]fullExport{
		"rich":  diagramFixture(),
		"empty": {VPC: VPCInfo{ID: "vpc-empty"}},
	} {
		svg := vpcDiagramSVG(data)
		dec := xml.NewDecoder(strings.NewReader(svg))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("%s: malformed SVG: %v", name, err)
			}
		}
	}
}

func TestVPCDiagramSVGEmpty(t *testing.T) {
	svg := vpcDiagramSVG(fullExport{VPC: VPCInfo{ID: "vpc-empty"}})
	if !strings.Contains(svg, "<svg") {
		t.Error("empty VPC should still produce an SVG")
	}
	if !strings.Contains(svg, "No subnets in this VPC") {
		t.Errorf("empty VPC diagram should note the absence of subnets:\n%s", svg)
	}
	// No IGW in the snapshot → no internet/gateway nodes.
	if strings.Contains(svg, "INTERNET") {
		t.Error("a VPC without an internet gateway should not show the internet node")
	}
}

func TestVPCDiagramSVGDeterministic(t *testing.T) {
	a := vpcDiagramSVG(diagramFixture())
	b := vpcDiagramSVG(diagramFixture())
	if a != b {
		t.Error("diagram output should be deterministic for the same input")
	}
}

// bigFixture is a 40-subnet, 3-AZ VPC with an IGW and a NAT — the scale case.
func bigFixture() fullExport {
	f := fullExport{
		VPC:  VPCInfo{ID: "vpc-big", CIDR: "10.0.0.0/16"},
		Snap: vpcSnapshot{VPCID: "vpc-big", InternetGateways: []IGWInfo{{ID: "igw-9"}}, NatGateways: []NatGWInfo{{ID: "nat-9", SubnetID: "subnet-0"}}},
	}
	azs := []string{"eu-west-1a", "eu-west-1b", "eu-west-1c"}
	pub := RouteTableInfo{ID: "rtb-pub", Routes: []Route{{Destination: "0.0.0.0/0", Target: "igw-9"}}}
	priv := RouteTableInfo{ID: "rtb-priv", IsMain: true, Routes: []Route{{Destination: "0.0.0.0/0", Target: "nat-9"}}}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("subnet-%d", i)
		f.Snap.Subnets = append(f.Snap.Subnets, SubnetInfo{ID: id, CIDR: fmt.Sprintf("10.0.%d.0/24", i), AZ: azs[i%3]})
		if i%2 == 0 {
			pub.Associations = append(pub.Associations, id)
		} else {
			priv.Associations = append(priv.Associations, id)
		}
	}
	f.Snap.RouteTables = []RouteTableInfo{pub, priv}
	return f
}

// assertSVGWithinViewBox checks no box or connector is drawn outside the canvas
// — the guard against an off-canvas "junk" diagram. Rects and lines/polylines
// are bounds-checked strictly; text anchor points loosely.
func assertSVGWithinViewBox(t *testing.T, svg string) {
	t.Helper()
	var w, h int
	dec := xml.NewDecoder(strings.NewReader(svg))
	num := func(attrs []xml.Attr, name string) (int, bool) {
		for _, a := range attrs {
			if a.Name.Local == name {
				v, err := strconv.Atoi(a.Value)
				return v, err == nil
			}
		}
		return 0, false
	}
	within := func(x, y int) bool { return x >= 0 && y >= 0 && x <= w && y <= h }

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("malformed SVG: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "svg":
			// viewBox="0 0 W H"
			for _, a := range se.Attr {
				if a.Name.Local == "viewBox" {
					f := strings.Fields(a.Value)
					if len(f) == 4 {
						w, _ = strconv.Atoi(f[2])
						h, _ = strconv.Atoi(f[3])
					}
				}
			}
			if w == 0 || h == 0 {
				t.Fatal("viewBox missing or zero")
			}
		case "rect":
			x, _ := num(se.Attr, "x")
			y, _ := num(se.Attr, "y")
			rw, _ := num(se.Attr, "width")
			rh, _ := num(se.Attr, "height")
			if !within(x, y) || !within(x+rw, y+rh) {
				t.Errorf("rect (%d,%d %dx%d) outside viewBox %dx%d", x, y, rw, rh, w, h)
			}
		case "line":
			x1, _ := num(se.Attr, "x1")
			y1, _ := num(se.Attr, "y1")
			x2, _ := num(se.Attr, "x2")
			y2, _ := num(se.Attr, "y2")
			if !within(x1, y1) || !within(x2, y2) {
				t.Errorf("line (%d,%d)->(%d,%d) outside viewBox %dx%d", x1, y1, x2, y2, w, h)
			}
		case "polyline":
			for _, a := range se.Attr {
				if a.Name.Local != "points" {
					continue
				}
				for _, pt := range strings.Fields(a.Value) {
					xy := strings.Split(pt, ",")
					if len(xy) != 2 {
						continue
					}
					px, _ := strconv.Atoi(xy[0])
					py, _ := strconv.Atoi(xy[1])
					if !within(px, py) {
						t.Errorf("polyline point (%d,%d) outside viewBox %dx%d", px, py, w, h)
					}
				}
			}
		case "text":
			x, _ := num(se.Attr, "x")
			y, _ := num(se.Attr, "y")
			if !within(x, y) {
				t.Errorf("text anchor (%d,%d) outside viewBox %dx%d", x, y, w, h)
			}
		}
	}
}

func TestVPCDiagramSVGWithinViewBox(t *testing.T) {
	assertSVGWithinViewBox(t, vpcDiagramSVG(diagramFixture()))
}

// TestVPCDiagramSVGLargeWithinViewBox proves the lane-wrapping layout keeps a
// 40-subnet VPC inside the canvas (no overlap/off-canvas at scale).
func TestVPCDiagramSVGLargeWithinViewBox(t *testing.T) {
	svg := vpcDiagramSVG(bigFixture())
	assertSVGWithinViewBox(t, svg)
	// Sanity: it should still be well-formed and contain all 40 subnets.
	if c := strings.Count(svg, `data-layer="subnets"`); c != 1 {
		t.Errorf("expected exactly one subnet layer group, got %d", c)
	}
	for _, id := range []string{"subnet-0", "subnet-39"} {
		if !strings.Contains(svg, id) {
			t.Errorf("large diagram missing %q", id)
		}
	}
}

// TestSubnetEgress classifies subnets by their default route.
func TestSubnetEgress(t *testing.T) {
	eg := subnetEgress(diagramFixture().Snap)
	cases := map[string]string{
		"subnet-pub-a":  "igw",
		"subnet-priv-a": "nat",
	}
	for id, want := range cases {
		if got := eg[id].kind; got != want {
			t.Errorf("egress[%s].kind = %q, want %q", id, got, want)
		}
	}
	// A subnet with no matching route table and no main table is isolated.
	iso := subnetEgress(vpcSnapshot{Subnets: []SubnetInfo{{ID: "s-x"}}})
	if iso["s-x"].kind != "none" {
		t.Errorf("unrouted subnet kind = %q, want none", iso["s-x"].kind)
	}
}

// The .svg file is opened outside the report, where nothing resolves the
// diagram's classes — so it has to carry the stylesheet itself, and still be
// well-formed XML with it embedded.
func TestStandaloneDiagramSVGCarriesItsStyles(t *testing.T) {
	svg := standaloneDiagramSVG(relationFixture())
	if !strings.Contains(svg, "<style><![CDATA[") {
		t.Error("standalone SVG has no embedded stylesheet")
	}
	for _, want := range []string{".vpcd .pub .box", "prefers-color-scheme: dark"} {
		if !strings.Contains(svg, want) {
			t.Errorf("embedded stylesheet missing %q", want)
		}
	}
	dec := xml.NewDecoder(strings.NewReader(svg))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("standalone SVG is malformed: %v", err)
		}
	}
	// The inline copy stays clean: the report's own <head> styles it.
	if strings.Contains(vpcDiagramSVG(relationFixture()), "<style>") {
		t.Error("the inline diagram should not repeat the report's stylesheet")
	}
}
