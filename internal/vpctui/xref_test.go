package vpctui

import (
	"strings"
	"testing"
)

func xrefSnap() vpcSnapshot {
	return vpcSnapshot{
		VPCID: "vpc-1",
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-a", SubnetID: "subnet-pub", AttachedTo: "i-web", SecurityGroups: []string{"sg-web"}},
			{ID: "eni-b", SubnetID: "subnet-priv", AttachedTo: "i-db", SecurityGroups: []string{"sg-db"}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-web", Name: "web", Rules: []SGRule{
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
			{ID: "sg-db", Name: "db", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "sg-web"},
			}},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-pub", IsMain: true, Associations: []string{"subnet-pub"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
				{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"},
			}},
			{ID: "rtb-priv", Associations: []string{"subnet-priv"}, Routes: []Route{
				{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"},
			}},
		},
		NatGateways: []NatGWInfo{{ID: "nat-1", SubnetID: "subnet-pub", State: "available"}},
		NetworkACLs: []NACLInfo{
			{ID: "acl-default", IsDefault: true},
			{ID: "acl-priv", Associations: []string{"subnet-priv"}},
		},
	}
}

// group returns the items of the named group, or nil.
func group(groups []xrefGroup, label string) []string {
	for _, g := range groups {
		if g.Label == label {
			return g.Items
		}
	}
	return nil
}

func TestXrefSecurityGroup(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "sg-web")
	enis := group(g, "Attached to network interfaces")
	if len(enis) != 1 || !strings.Contains(enis[0], "eni-a") || !strings.Contains(enis[0], "i-web") {
		t.Errorf("expected eni-a → i-web, got %v", enis)
	}
	refs := group(g, "Referenced by other security groups")
	if len(refs) != 1 || !strings.Contains(refs[0], "sg-db") {
		t.Errorf("expected sg-db referencing sg-web, got %v", refs)
	}
}

func TestXrefSubnet(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "subnet-priv")
	if rt := group(g, "Route table"); len(rt) != 1 || !strings.Contains(rt[0], "rtb-priv") {
		t.Errorf("expected rtb-priv, got %v", rt)
	}
	if nacl := group(g, "Network ACL"); len(nacl) != 1 || !strings.Contains(nacl[0], "acl-priv") {
		t.Errorf("expected acl-priv, got %v", nacl)
	}
	if enis := group(g, "Network interfaces in subnet"); len(enis) != 1 || enis[0] != "eni-b" {
		t.Errorf("expected eni-b, got %v", enis)
	}
}

func TestXrefSubnetImplicitMainAndDefault(t *testing.T) {
	// subnet-pub is explicitly associated to the main RT; but test a subnet with
	// no explicit NACL association -> default NACL should be reported.
	g, _ := crossReference(xrefSnap(), "subnet-pub")
	nacl := group(g, "Network ACL")
	if len(nacl) != 1 || !strings.Contains(nacl[0], "acl-default") || !strings.Contains(nacl[0], "default") {
		t.Errorf("expected implicit default NACL, got %v", nacl)
	}
	if nat := group(g, "NAT gateways in subnet"); len(nat) != 1 || !strings.Contains(nat[0], "nat-1") {
		t.Errorf("expected nat-1 in subnet-pub, got %v", nat)
	}
}

func TestXrefSubnetImplicitMainRoute(t *testing.T) {
	snap := xrefSnap()
	// Remove explicit association of subnet-priv so it falls back to main RT.
	snap.RouteTables[1].Associations = nil
	g, _ := crossReference(snap, "subnet-priv")
	rt := group(g, "Route table")
	if len(rt) != 1 || !strings.Contains(rt[0], "rtb-pub") || !strings.Contains(rt[0], "main") {
		t.Errorf("expected implicit main rtb-pub, got %v", rt)
	}
}

func TestXrefRouteTable(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "rtb-pub")
	if subs := group(g, "Associated subnets"); len(subs) != 1 || subs[0] != "subnet-pub" {
		t.Errorf("expected subnet-pub, got %v", subs)
	}
	targets := group(g, "Routes to")
	if len(targets) != 1 || !strings.Contains(targets[0], "igw-1") {
		t.Errorf("expected igw-1 target (local excluded), got %v", targets)
	}
}

func TestXrefNatGateway(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "nat-1")
	if sub := group(g, "In subnet"); len(sub) != 1 || sub[0] != "subnet-pub" {
		t.Errorf("expected subnet-pub, got %v", sub)
	}
	routedBy := group(g, "Routed to by route tables")
	if len(routedBy) != 1 || !strings.Contains(routedBy[0], "rtb-priv") {
		t.Errorf("expected rtb-priv routing to nat-1, got %v", routedBy)
	}
}

func TestXrefInternetGateway(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "igw-1")
	routedBy := group(g, "Routed to by route tables")
	if len(routedBy) != 1 || !strings.Contains(routedBy[0], "rtb-pub") {
		t.Errorf("expected rtb-pub routing to igw-1, got %v", routedBy)
	}
}

func TestXrefENI(t *testing.T) {
	g, _ := crossReference(xrefSnap(), "eni-a")
	if a := group(g, "Attached to"); len(a) != 1 || a[0] != "i-web" {
		t.Errorf("expected i-web, got %v", a)
	}
	if sg := group(g, "Security groups"); len(sg) != 1 || sg[0] != "sg-web" {
		t.Errorf("expected sg-web, got %v", sg)
	}
}

func TestXrefUnknownPrefix(t *testing.T) {
	g, supported := crossReference(xrefSnap(), "fl-123")
	if supported || g != nil {
		t.Errorf("unknown prefix should be reported unsupported, got %v (supported=%v)", g, supported)
	}
}

func TestXrefEndpoint(t *testing.T) {
	snap := xrefSnap()
	snap.Endpoints = []EndpointInfo{{
		ID:            "vpce-1",
		ServiceName:   "com.amazonaws.us-east-1.s3",
		Type:          "Gateway",
		RouteTableIDs: []string{"rtb-priv"},
	}}
	g, supported := crossReference(snap, "vpce-1")
	if !supported {
		t.Fatal("VPC endpoints should be cross-referenced")
	}
	if svc := group(g, "Service"); len(svc) != 1 || !strings.Contains(svc[0], "s3") {
		t.Errorf("expected the endpoint's service, got %v", svc)
	}
	if rts := group(g, "Associated route tables"); len(rts) != 1 || rts[0] != "rtb-priv" {
		t.Errorf("expected rtb-priv association, got %v", rts)
	}
}

func TestXrefPeering(t *testing.T) {
	snap := xrefSnap()
	snap.Peerings = []PeeringInfo{{
		ID:             "pcx-1",
		RequesterVPCID: "vpc-a",
		RequesterCIDR:  "10.0.0.0/16",
		AccepterVPCID:  "vpc-b",
		AccepterCIDR:   "172.16.0.0/16",
	}}
	snap.RouteTables = append(snap.RouteTables, RouteTableInfo{
		ID: "rtb-peer", Routes: []Route{{Destination: "172.16.0.0/16", Target: "pcx-1", State: "active"}},
	})
	g, supported := crossReference(snap, "pcx-1")
	if !supported {
		t.Fatal("peering connections should be cross-referenced")
	}
	sides := group(g, "Peered VPCs")
	if len(sides) != 2 || !strings.Contains(sides[0], "vpc-a") || !strings.Contains(sides[1], "172.16.0.0/16") {
		t.Errorf("expected both peering sides with CIDRs, got %v", sides)
	}
	if routedBy := group(g, "Routed to by route tables"); len(routedBy) != 1 || !strings.Contains(routedBy[0], "rtb-peer") {
		t.Errorf("expected rtb-peer routing to pcx-1, got %v", routedBy)
	}
}

func TestXrefEmptyGroupsOmitted(t *testing.T) {
	// An SG referenced by nothing should yield no groups.
	snap := xrefSnap()
	g, _ := crossReference(snap, "sg-db") // sg-db is used by eni-b but referenced by no SG
	if items := group(g, "Referenced by other security groups"); items != nil {
		t.Errorf("empty group should be omitted, got %v", items)
	}
	if items := group(g, "Attached to network interfaces"); len(items) != 1 {
		t.Errorf("sg-db should still show its ENI, got %v", items)
	}
}
