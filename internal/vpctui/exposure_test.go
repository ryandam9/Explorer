package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func exposureSnap() vpcSnapshot {
	return vpcSnapshot{
		VPCID: "vpc-1",
		Subnets: []SubnetInfo{
			{ID: "subnet-pub", CIDR: "10.0.0.0/24"},
			{ID: "subnet-priv", CIDR: "10.0.1.0/24"},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-pub", Associations: []string{"subnet-pub"}, Routes: []Route{
				{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"},
			}},
			{ID: "rtb-priv", Associations: []string{"subnet-priv"}, Routes: []Route{
				{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"},
			}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-web", Name: "web", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
				{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/8"}, // private, not exposed
			}},
			{ID: "sg-internal", Name: "internal", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "sg-web"},
			}},
		},
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-pub", PublicIP: "52.1.1.1", SubnetID: "subnet-pub", AttachedTo: "i-web", SecurityGroups: []string{"sg-web"}},
			{ID: "eni-priv", SubnetID: "subnet-priv", AttachedTo: "i-db", SecurityGroups: []string{"sg-internal"}},
		},
	}
}

func TestExposureReport(t *testing.T) {
	groups := exposureReport(exposureSnap())

	subnets := group(groups, "Public subnets (route to an internet gateway)")
	if len(subnets) != 1 || !strings.Contains(subnets[0], "subnet-pub") {
		t.Errorf("expected only subnet-pub public, got %v", subnets)
	}
	sgs := group(groups, "Security groups open to the internet (inbound from 0.0.0.0/0)")
	if len(sgs) != 1 || !strings.Contains(sgs[0], "sg-web") || !strings.Contains(sgs[0], "HTTPS (TCP 443)") {
		t.Errorf("expected sg-web open on 443, got %v", sgs)
	}
	// sg-internal must not appear (its source is another SG, not the internet).
	for _, s := range sgs {
		if strings.Contains(s, "sg-internal") {
			t.Errorf("sg-internal should not be internet-exposed: %v", sgs)
		}
	}
	enis := group(groups, "Network interfaces with a public IP")
	if len(enis) != 1 || !strings.Contains(enis[0], "eni-pub") || !strings.Contains(enis[0], "52.1.1.1") {
		t.Errorf("expected eni-pub with public IP, got %v", enis)
	}
}

func TestExposureReportEmpty(t *testing.T) {
	// A fully private VPC yields no exposure groups.
	snap := vpcSnapshot{
		Subnets:     []SubnetInfo{{ID: "subnet-1", CIDR: "10.0.0.0/24"}},
		RouteTables: []RouteTableInfo{{ID: "rtb-1", IsMain: true, Routes: []Route{{Destination: "10.0.0.0/16", Target: "local", State: "active"}}}},
		SecurityGroups: []SGInfo{{ID: "sg-1", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "10.0.0.0/8"},
		}}},
		NetworkInterfaces: []ENIInfo{{ID: "eni-1", SubnetID: "subnet-1"}},
	}
	if g := exposureReport(snap); len(g) != 0 {
		t.Errorf("private VPC should have no exposure, got %v", g)
	}
}

func TestRenderExposure(t *testing.T) {
	m := &Model{exposureGroups: exposureReport(exposureSnap())}
	m.exposureVP = viewport.New(80, 20)
	out := ansi.Strip(m.renderExposure())
	if !strings.Contains(out, "Public subnets") || !strings.Contains(out, "eni-pub") {
		t.Errorf("exposure render incomplete:\n%s", out)
	}
}

func TestRenderExposureEmpty(t *testing.T) {
	m := &Model{}
	if !strings.Contains(ansi.Strip(m.renderExposure()), "Nothing in this VPC is reachable") {
		t.Error("empty exposure should show a clean message")
	}
}

func TestCheckOrphans(t *testing.T) {
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{
			{ID: "subnet-used", CIDR: "10.0.0.0/24"},
			{ID: "subnet-empty", CIDR: "10.0.1.0/24"},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-used", Name: "used"},
			{ID: "sg-orphan", Name: "orphan"},
			{ID: "sg-default", Name: "default"}, // default is exempt
		},
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-1", SubnetID: "subnet-used", SecurityGroups: []string{"sg-used"}},
		},
	}
	var out []Finding
	checkOrphans(snap, &out)

	if findOneRes(out, "sg-orphan", "appears unused") == nil {
		t.Error("expected sg-orphan flagged as unused")
	}
	if findOneRes(out, "sg-used", "appears unused") != nil {
		t.Error("sg-used is attached and should not be flagged")
	}
	if findOneRes(out, "sg-default", "appears unused") != nil {
		t.Error("the default SG should be exempt")
	}
	if findOneRes(out, "subnet-empty", "no network interfaces") == nil {
		t.Error("expected subnet-empty flagged")
	}
	if findOneRes(out, "subnet-used", "no network interfaces") != nil {
		t.Error("subnet-used has an ENI and should not be flagged")
	}
}

func TestCheckOrphansSkippedWithoutENIs(t *testing.T) {
	// No ENI data -> skip to avoid false "unused" findings from a partial snapshot.
	snap := vpcSnapshot{SecurityGroups: []SGInfo{{ID: "sg-1"}}, Subnets: []SubnetInfo{{ID: "subnet-1"}}}
	var out []Finding
	checkOrphans(snap, &out)
	if len(out) != 0 {
		t.Errorf("orphan checks should be skipped without ENIs, got %+v", out)
	}
}
