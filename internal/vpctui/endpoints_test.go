package vpctui

import "testing"

func TestCheckEndpointsGatewayNoRouteTable(t *testing.T) {
	snap := vpcSnapshot{Endpoints: []EndpointInfo{
		{ID: "vpce-gw", ServiceName: "com.amazonaws.region.s3", Type: "Gateway", State: "available"},
		{ID: "vpce-gw-ok", ServiceName: "com.amazonaws.region.dynamodb", Type: "Gateway", State: "available", RouteTableIDs: []string{"rtb-1"}},
	}}
	var out []Finding
	checkEndpoints(snap, &out)
	if f := findOneRes(out, "vpce-gw", "not associated with any route table"); f == nil || f.Severity != SevWarning {
		t.Errorf("expected unassociated gateway endpoint warning, got %+v", f)
	}
	if f := findOneRes(out, "vpce-gw-ok", "not associated with any route table"); f != nil {
		t.Errorf("associated gateway endpoint should not be flagged: %+v", f)
	}
}

func TestCheckEndpointsInterfaceSGAndDNS(t *testing.T) {
	snap := vpcSnapshot{
		SecurityGroups: []SGInfo{
			{ID: "sg-allow", Rules: []SGRule{{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "10.0.0.0/16"}}},
			{ID: "sg-block", Rules: []SGRule{{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/16"}}},
		},
		Endpoints: []EndpointInfo{
			{ID: "vpce-ok", Type: "Interface", State: "available", SecurityGroups: []string{"sg-allow"}, PrivateDNSEnabled: true},
			{ID: "vpce-noson", Type: "Interface", State: "available", SecurityGroups: []string{"sg-block"}, PrivateDNSEnabled: true},
			{ID: "vpce-nodns", Type: "Interface", State: "available", SecurityGroups: []string{"sg-allow"}, PrivateDNSEnabled: false},
		},
	}
	var out []Finding
	checkEndpoints(snap, &out)

	if f := findOneRes(out, "vpce-noson", "do not allow inbound HTTPS"); f == nil || f.Severity != SevWarning {
		t.Errorf("expected HTTPS-blocked warning for vpce-noson, got %+v", f)
	}
	if f := findOneRes(out, "vpce-ok", "do not allow inbound HTTPS"); f != nil {
		t.Errorf("vpce-ok allows 443 and should not be flagged: %+v", f)
	}
	if f := findOneRes(out, "vpce-nodns", "private DNS disabled"); f == nil || f.Severity != SevInfo {
		t.Errorf("expected private-DNS-disabled info for vpce-nodns, got %+v", f)
	}
	if f := findOneRes(out, "vpce-ok", "private DNS disabled"); f != nil {
		t.Errorf("vpce-ok has private DNS on and should not be flagged: %+v", f)
	}
}

func TestCheckEndpointsState(t *testing.T) {
	snap := vpcSnapshot{Endpoints: []EndpointInfo{
		{ID: "vpce-pending", Type: "Gateway", State: "pending", RouteTableIDs: []string{"rtb-1"}},
	}}
	var out []Finding
	checkEndpoints(snap, &out)
	if findOneRes(out, "vpce-pending", "not in the available state") == nil {
		t.Error("expected a not-available finding for the pending endpoint")
	}
}

func TestEndpointToMapEnriched(t *testing.T) {
	m := endpointToMap(EndpointInfo{
		ID: "vpce-1", ServiceName: "svc", Type: "Interface", State: "available",
		RouteTableIDs: nil, SubnetIDs: []string{"subnet-a", "subnet-b"},
		SecurityGroups: []string{"sg-1"}, PrivateDNSEnabled: true,
	})
	if m["subnets"] != "subnet-a, subnet-b" {
		t.Errorf("subnets = %q", m["subnets"])
	}
	if m["security_groups"] != "sg-1" {
		t.Errorf("security_groups = %q", m["security_groups"])
	}
	if m["private_dns"] != "Yes" {
		t.Errorf("private_dns = %q", m["private_dns"])
	}
	if m["route_tables"] != "-" { // empty -> dash
		t.Errorf("route_tables = %q, want -", m["route_tables"])
	}
}
