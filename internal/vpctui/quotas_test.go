package vpctui

import (
	"strings"
	"testing"
)

func sgWithRules(id string, inbound, outbound int) SGInfo {
	sg := SGInfo{ID: id}
	for i := 0; i < inbound; i++ {
		sg.Rules = append(sg.Rules, SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "10.0.0.0/8"})
	}
	for i := 0; i < outbound; i++ {
		sg.Rules = append(sg.Rules, SGRule{Direction: "Outbound", Protocol: "TCP", PortRange: "443", Source: "10.0.0.0/8"})
	}
	return sg
}

func TestCheckQuotasSGRules(t *testing.T) {
	snap := vpcSnapshot{SecurityGroups: []SGInfo{
		sgWithRules("sg-ok", 10, 10),  // well under -> no finding
		sgWithRules("sg-warn", 50, 0), // 50/60 = warn
		sgWithRules("sg-crit", 60, 0), // at limit -> critical
	}}
	var out []Finding
	checkQuotas(snap, &out)

	if f := findOneRes(out, "sg-ok", "rule limit"); f != nil {
		t.Errorf("sg-ok should not be flagged: %+v", f)
	}
	if f := findOneRes(out, "sg-warn", "rule limit"); f == nil || f.Severity != SevWarning {
		t.Errorf("sg-warn should be a warning, got %+v", f)
	}
	if f := findOneRes(out, "sg-crit", "rule limit"); f == nil || f.Severity != SevCritical {
		t.Errorf("sg-crit should be critical, got %+v", f)
	}
}

func TestCheckQuotasRoutes(t *testing.T) {
	var routes []Route
	for i := 0; i < 50; i++ {
		routes = append(routes, Route{Destination: "10.0.0.0/16", Target: "local", State: "active"})
	}
	snap := vpcSnapshot{RouteTables: []RouteTableInfo{{ID: "rtb-full", Routes: routes}}}
	var out []Finding
	checkQuotas(snap, &out)
	f := findOneRes(out, "rtb-full", "route limit")
	if f == nil || f.Severity != SevCritical {
		t.Errorf("expected critical route-limit finding, got %+v", f)
	}
}

func TestCheckQuotasSGsPerENI(t *testing.T) {
	snap := vpcSnapshot{NetworkInterfaces: []ENIInfo{
		{ID: "eni-many", SecurityGroups: []string{"sg-1", "sg-2", "sg-3", "sg-4", "sg-5"}},
		{ID: "eni-few", SecurityGroups: []string{"sg-1"}},
	}}
	var out []Finding
	checkQuotas(snap, &out)
	if f := findOneRes(out, "eni-many", "security-group limit"); f == nil || f.Severity != SevInfo {
		t.Errorf("eni-many should get an info finding, got %+v", f)
	}
	if f := findOneRes(out, "eni-few", "security-group limit"); f != nil {
		t.Errorf("eni-few should not be flagged: %+v", f)
	}
}

func TestCheckQuotasSubnetsPerVPC(t *testing.T) {
	snap := vpcSnapshot{VPCID: "vpc-9"}
	for i := 0; i < 200; i++ {
		snap.Subnets = append(snap.Subnets, SubnetInfo{ID: "subnet", CIDR: "10.0.0.0/24", AvailableIPs: 200})
	}
	var out []Finding
	checkQuotas(snap, &out)
	f := findOneRes(out, "vpc-9", "subnet limit")
	if f == nil || f.Severity != SevCritical {
		t.Errorf("expected critical subnet-limit finding, got %+v", f)
	}
}

func TestCheckQuotasNoneWhenSmall(t *testing.T) {
	snap := vpcSnapshot{
		SecurityGroups:    []SGInfo{sgWithRules("sg-1", 3, 3)},
		RouteTables:       []RouteTableInfo{{ID: "rtb-1", Routes: []Route{{Destination: "0.0.0.0/0", Target: "igw-1"}}}},
		NetworkInterfaces: []ENIInfo{{ID: "eni-1", SecurityGroups: []string{"sg-1"}}},
		Subnets:           []SubnetInfo{{ID: "subnet-1", CIDR: "10.0.0.0/24", AvailableIPs: 200}},
	}
	var out []Finding
	checkQuotas(snap, &out)
	if len(out) != 0 {
		t.Errorf("small VPC should produce no quota findings, got %d: %+v", len(out), out)
	}
}

// findOneRes returns the first finding matching a resource and title substring.
func findOneRes(fs []Finding, resource, titleSub string) *Finding {
	for i := range fs {
		if fs[i].Resource == resource && strings.Contains(fs[i].Title, titleSub) {
			return &fs[i]
		}
	}
	return nil
}
