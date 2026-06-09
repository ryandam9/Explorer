package vpctui

import (
	"strings"
	"testing"
)

// findOne returns the first finding whose title contains sub, or nil.
func findOne(fs []Finding, sub string) *Finding {
	for i := range fs {
		if strings.Contains(fs[i].Title, sub) {
			return &fs[i]
		}
	}
	return nil
}

func countTitle(fs []Finding, sub string) int {
	n := 0
	for _, f := range fs {
		if strings.Contains(f.Title, sub) {
			n++
		}
	}
	return n
}

func TestUsableIPs(t *testing.T) {
	cases := map[string]int{
		"172.31.0.0/20": 4091, // 4096 - 5
		"10.0.0.0/24":   251,  // 256 - 5
		"10.0.0.0/28":   11,   // 16 - 5
		"10.0.0.0/30":   0,    // 4 - 5 -> clamped to 0
		"bogus":         0,
	}
	for cidr, want := range cases {
		if got := usableIPs(cidr); got != want {
			t.Errorf("usableIPs(%q) = %d, want %d", cidr, got, want)
		}
	}
}

func TestCIDRsOverlap(t *testing.T) {
	overlap := [][2]string{
		{"10.0.0.0/16", "10.0.1.0/24"},
		{"10.0.0.0/16", "10.0.0.0/8"},
		{"192.168.0.0/24", "192.168.0.0/24"},
	}
	noOverlap := [][2]string{
		{"10.0.0.0/16", "10.1.0.0/16"},
		{"10.0.0.0/24", "10.0.1.0/24"},
		{"", "10.0.0.0/8"},
	}
	for _, c := range overlap {
		if !cidrsOverlap(c[0], c[1]) {
			t.Errorf("cidrsOverlap(%q,%q) = false, want true", c[0], c[1])
		}
	}
	for _, c := range noOverlap {
		if cidrsOverlap(c[0], c[1]) {
			t.Errorf("cidrsOverlap(%q,%q) = true, want false", c[0], c[1])
		}
	}
}

func TestCheckSecurityGroups(t *testing.T) {
	snap := vpcSnapshot{SecurityGroups: []SGInfo{
		{ID: "sg-web", Name: "web", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"}, // ok, not flagged
			{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"},  // critical (admin)
		}},
		{ID: "sg-db", Name: "db", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "0.0.0.0/0"},  // critical (data)
			{Direction: "Inbound", Protocol: "TCP", PortRange: "5432", Source: "sg-missing"}, // info: unknown sg ref
		}},
	}}
	var out []Finding
	checkSecurityGroups(snap, &out)

	if n := countTitle(out, "exposes a sensitive port"); n != 2 {
		t.Fatalf("expected 2 exposure findings, got %d (%+v)", n, out)
	}
	for _, f := range out {
		if strings.Contains(f.Title, "exposes") && f.Severity != SevCritical {
			t.Errorf("admin/data exposure should be critical, got %v for %s", f.Severity, f.Detail)
		}
	}
	if ref := findOne(out, "references a security group not found"); ref == nil {
		t.Error("expected an unknown-SG-reference finding")
	} else if ref.Severity != SevInfo {
		t.Errorf("unknown SG ref should be info, got %v", ref.Severity)
	}
	// Port 443 from the internet must NOT be flagged.
	for _, f := range out {
		if strings.Contains(f.Detail, "HTTPS") {
			t.Errorf("HTTPS from internet should not be flagged: %s", f.Detail)
		}
	}
}

func TestCheckRouteTablesBlackhole(t *testing.T) {
	snap := vpcSnapshot{RouteTables: []RouteTableInfo{
		{ID: "rtb-1", Routes: []Route{
			{Destination: "10.0.0.0/16", Target: "local", State: "active"},
			{Destination: "0.0.0.0/0", Target: "nat-dead", State: "blackhole"},
		}},
	}}
	var out []Finding
	checkRouteTables(snap, &out)
	f := findOne(out, "blackhole route")
	if f == nil {
		t.Fatal("expected a blackhole finding")
	}
	if f.Severity != SevWarning {
		t.Errorf("blackhole should be warning, got %v", f.Severity)
	}
	if !strings.Contains(f.Detail, "0.0.0.0/0") {
		t.Errorf("detail should mention the destination: %s", f.Detail)
	}
}

func TestCheckSubnetsCapacity(t *testing.T) {
	snap := vpcSnapshot{Subnets: []SubnetInfo{
		{ID: "subnet-low", CIDR: "10.0.0.0/28", AvailableIPs: 2},  // low absolute + high util
		{ID: "subnet-ok", CIDR: "10.0.1.0/24", AvailableIPs: 200}, // fine
	}}
	var out []Finding
	checkSubnets(snap, &out)
	if countTitle(out, "running low on IP") != 1 {
		t.Fatalf("expected exactly 1 low-IP finding, got %d (%+v)", countTitle(out, "running low on IP"), out)
	}
	f := findOne(out, "running low on IP")
	if f.Resource != "subnet-low" {
		t.Errorf("wrong subnet flagged: %s", f.Resource)
	}
}

func TestCheckSubnetsRouting(t *testing.T) {
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{
			{ID: "subnet-pub", CIDR: "10.0.0.0/24", AvailableIPs: 200, MapPublicIPOnLaunch: true},
			{ID: "subnet-iso", CIDR: "10.0.1.0/24", AvailableIPs: 200},
		},
		RouteTables: []RouteTableInfo{
			// Main table: only local route -> both subnets are isolated unless associated.
			{ID: "rtb-main", IsMain: true, Routes: []Route{{Destination: "10.0.0.0/16", Target: "local", State: "active"}}},
		},
	}
	var out []Finding
	checkSubnets(snap, &out)

	// subnet-pub auto-assigns public IP but has no IGW route.
	if findOne(out, "auto-assigns public IPs but has no internet gateway") == nil {
		t.Error("expected public-IP-without-IGW finding")
	}
	// Both subnets have no outbound internet path.
	if countTitle(out, "no outbound internet path") != 2 {
		t.Errorf("expected 2 no-egress findings, got %d", countTitle(out, "no outbound internet path"))
	}

	// Now give subnet-pub a real IGW route via association; the warnings should clear.
	snap.RouteTables = append(snap.RouteTables, RouteTableInfo{
		ID: "rtb-pub", Associations: []string{"subnet-pub"},
		Routes: []Route{
			{Destination: "10.0.0.0/16", Target: "local", State: "active"},
			{Destination: "0.0.0.0/0", Target: "igw-123", State: "active"},
		},
	})
	out = nil
	checkSubnets(snap, &out)
	if findOne(out, "auto-assigns public IPs but has no internet gateway") != nil {
		t.Error("public subnet with IGW route should not be flagged")
	}
	if countTitle(out, "no outbound internet path") != 1 {
		t.Errorf("only subnet-iso should be flagged now, got %d", countTitle(out, "no outbound internet path"))
	}
}

func TestCheckNatGateways(t *testing.T) {
	snap := vpcSnapshot{
		NatGateways: []NatGWInfo{
			{ID: "nat-used", State: "available"},
			{ID: "nat-idle", State: "available"},
			{ID: "nat-bad", State: "failed"},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-1", Routes: []Route{{Destination: "0.0.0.0/0", Target: "nat-used", State: "active"}}},
		},
	}
	var out []Finding
	checkNatGateways(snap, &out)

	idle := findOne(out, "not referenced by any route")
	if idle == nil || idle.Resource != "nat-idle" {
		t.Errorf("expected nat-idle flagged as unreferenced, got %+v", out)
	}
	if findOne(out, "not in the available state") == nil {
		t.Error("expected nat-bad state finding")
	}
	// nat-used must not be flagged as idle.
	for _, f := range out {
		if f.Resource == "nat-used" {
			t.Errorf("nat-used should not be flagged: %+v", f)
		}
	}
}

func TestCheckNACLsStateless(t *testing.T) {
	// Custom NACL allowing inbound HTTPS but no outbound ephemeral allow.
	snap := vpcSnapshot{NetworkACLs: []NACLInfo{
		{ID: "acl-custom", IsDefault: false, Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			{RuleNumber: 32767, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Outbound"},
		}},
		{ID: "acl-default", IsDefault: true, Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		}},
	}}
	var out []Finding
	checkNACLs(snap, &out)
	f := findOne(out, "may block return traffic")
	if f == nil || f.Resource != "acl-custom" {
		t.Fatalf("expected return-traffic finding for acl-custom, got %+v", out)
	}

	// Adding an outbound ephemeral allow clears it.
	snap.NetworkACLs[0].Rules = append(snap.NetworkACLs[0].Rules, NACLRule{
		RuleNumber: 200, Protocol: "TCP", PortRange: "1024-65535", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound",
	})
	out = nil
	checkNACLs(snap, &out)
	if findOne(out, "may block return traffic") != nil {
		t.Error("ephemeral outbound allow should clear the return-traffic finding")
	}
}

func TestCheckPeerings(t *testing.T) {
	snap := vpcSnapshot{Peerings: []PeeringInfo{
		{ID: "pcx-overlap", Status: "active", RequesterCIDR: "10.0.0.0/16", AccepterCIDR: "10.0.5.0/24"},
		{ID: "pcx-pending", Status: "pending-acceptance", RequesterCIDR: "10.0.0.0/16", AccepterCIDR: "192.168.0.0/16"},
		{ID: "pcx-ok", Status: "active", RequesterCIDR: "10.0.0.0/16", AccepterCIDR: "172.16.0.0/16"},
	}}
	var out []Finding
	checkPeerings(snap, &out)
	if findOne(out, "overlapping CIDRs") == nil {
		t.Error("expected overlap finding for pcx-overlap")
	}
	if findOne(out, "not active") == nil {
		t.Error("expected not-active finding for pcx-pending")
	}
	for _, f := range out {
		if f.Resource == "pcx-ok" {
			t.Errorf("pcx-ok should not be flagged: %+v", f)
		}
	}
}

func TestAnalyzeVPCSortingAndCounts(t *testing.T) {
	snap := vpcSnapshot{
		SecurityGroups: []SGInfo{{ID: "sg-1", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"}, // critical
		}}},
		Peerings: []PeeringInfo{
			{ID: "pcx-1", Status: "pending", RequesterCIDR: "10.0.0.0/16", AccepterCIDR: "172.16.0.0/16"}, // info
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-1", Routes: []Route{{Destination: "0.0.0.0/0", Target: "x", State: "blackhole"}}}, // warning
		},
	}
	fs := analyzeVPC(snap)
	if len(fs) < 3 {
		t.Fatalf("expected at least 3 findings, got %d", len(fs))
	}
	// Sorted critical -> warning -> info.
	for i := 1; i < len(fs); i++ {
		if fs[i-1].Severity < fs[i].Severity {
			t.Errorf("findings not sorted by severity desc at %d: %v before %v", i, fs[i-1].Severity, fs[i].Severity)
		}
	}
	crit, warn, info := countBySeverity(fs)
	if crit < 1 || warn < 1 || info < 1 {
		t.Errorf("expected at least one of each severity, got crit=%d warn=%d info=%d", crit, warn, info)
	}
}

func TestAnalyzeVPCEmpty(t *testing.T) {
	if fs := analyzeVPC(vpcSnapshot{}); len(fs) != 0 {
		t.Errorf("empty snapshot should yield no findings, got %d", len(fs))
	}
}
