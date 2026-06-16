package vpctui

import (
	"encoding/xml"
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
			NatGateways:       []NatGWInfo{{ID: "nat-1", SubnetID: "subnet-pub-a", State: "available"}},
			NetworkInterfaces: []ENIInfo{{ID: "eni-1", SubnetID: "subnet-priv-a"}, {ID: "eni-2", SubnetID: "subnet-priv-a"}},
		},
	}
}

func TestVPCDiagramSVGStructure(t *testing.T) {
	svg := vpcDiagramSVG(diagramFixture())
	for _, want := range []string{
		"<svg",
		"viewBox=",
		`aria-label="VPC architecture diagram for vpc-1"`,
		`<marker id="arrow"`,
		"VPC · vpc-1",     // VPC container label
		"INTERNET",        // internet node
		"IGW · igw-1",     // gateway node
		"ap-southeast-2a", // AZ column headers
		"ap-southeast-2b",
		"subnet-pub-a", // subnet boxes
		"subnet-priv-b",
		"NAT · nat-1",           // NAT pill in its subnet
		"→ internet via igw-1",  // public-subnet egress tag
		"→ nat-1",               // private-subnet egress tag
		"2 ENIs",                // ENI count on subnet-priv-a
		"Public subnet (→ IGW)", // legend
		"Private subnet (→ NAT)",
		"NAT gateway",
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("diagram SVG missing %q", want)
		}
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

// TestVPCDiagramSVGWithinViewBox checks no box or connector is drawn outside
// the canvas — the guard against an off-canvas "junk" diagram. Rects and
// lines/polylines are bounds-checked strictly; text anchor points loosely.
func TestVPCDiagramSVGWithinViewBox(t *testing.T) {
	svg := vpcDiagramSVG(diagramFixture())

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
