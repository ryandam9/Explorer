package vpctui

import (
	"strings"
	"testing"
)

// Tests for the review fixes and enhancements to the VPC debugging proposals:
// direction-aware exposure findings, first-match NACL evaluation, smarter
// egress detection, same-subnet and internet-return path tracing, secondary
// IPs, peering secondary CIDRs, and the correlated exposure report.

func TestSGDefaultEgressNotFlagged(t *testing.T) {
	snap := vpcSnapshot{SecurityGroups: []SGInfo{
		{ID: "sg-egress-only", Rules: []SGRule{
			{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
		}},
		{ID: "sg-ssh-open", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"},
			{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
		}},
	}}
	var out []Finding
	checkSecurityGroups(snap, &out)

	for _, f := range out {
		if f.Resource == "sg-egress-only" {
			t.Errorf("the default allow-all egress rule must not be flagged as exposure: %+v", f)
		}
	}
	f := findOne(out, "exposes a sensitive port")
	if f == nil || f.Resource != "sg-ssh-open" || f.Severity != SevCritical {
		t.Errorf("expected critical inbound SSH exposure on sg-ssh-open, got %+v", out)
	}
}

func TestRiskSeveritySensitiveRangeIsCritical(t *testing.T) {
	// A range covering SSH must rank like SSH itself, not one level lower.
	note := exposureRisk("TCP", "20-30", "0.0.0.0/0")
	if note == "" {
		t.Fatal("expected a risk note for a range covering port 22")
	}
	if riskSeverity(note) != SevCritical {
		t.Errorf("range covering a sensitive port should be critical, got %v", riskSeverity(note))
	}
}

func TestNACLDenyShadowsEphemeralAllow(t *testing.T) {
	// First-match-wins: a deny-all at rule 100 shadows the allow at 200.
	shadowed := NACLInfo{ID: "acl-shadow", Rules: []NACLRule{
		{RuleNumber: 50, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Outbound"},
		{RuleNumber: 200, Protocol: "TCP", PortRange: "1024-65535", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
	}}
	var out []Finding
	checkNACLs(vpcSnapshot{NetworkACLs: []NACLInfo{shadowed}}, &out)
	if findOne(out, "may block return traffic") == nil {
		t.Error("a deny-all preceding the ephemeral allow should be flagged (first match wins)")
	}

	// A narrower deny leaves room for the later allow.
	narrow := shadowed
	narrow.Rules = append([]NACLRule(nil), shadowed.Rules...)
	narrow.Rules[1].CIDR = "192.0.2.0/24"
	out = nil
	checkNACLs(vpcSnapshot{NetworkACLs: []NACLInfo{narrow}}, &out)
	if findOne(out, "may block return traffic") != nil {
		t.Error("a narrow deny before the ephemeral allow should not be flagged")
	}
}

func TestNACLPartialEphemeralRangeFlagged(t *testing.T) {
	// An outbound allow that covers only the low ephemeral ports (1024-49151)
	// still drops return traffic on higher ports (e.g. 60999, 65535); probing
	// only 32768 would miss this, so it must be flagged.
	partial := NACLInfo{ID: "acl-partial", Rules: []NACLRule{
		{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		{RuleNumber: 200, Protocol: "TCP", PortRange: "1024-49151", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
	}}
	var out []Finding
	checkNACLs(vpcSnapshot{NetworkACLs: []NACLInfo{partial}}, &out)
	if findOne(out, "may block return traffic") == nil {
		t.Error("a partial ephemeral allow (1024-49151) should be flagged for higher return ports")
	}

	// The full range clears it.
	full := partial
	full.Rules = append([]NACLRule(nil), partial.Rules...)
	full.Rules[1].PortRange = "1024-65535"
	out = nil
	checkNACLs(vpcSnapshot{NetworkACLs: []NACLInfo{full}}, &out)
	if findOne(out, "may block return traffic") != nil {
		t.Error("the full ephemeral range should clear the finding")
	}
}

func TestHardenedDefaultNACLFlagged(t *testing.T) {
	// The default NACL's rules are editable; a hardened one must be linted too.
	snap := vpcSnapshot{NetworkACLs: []NACLInfo{
		{ID: "acl-default-hardened", IsDefault: true, Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			{RuleNumber: 32767, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Outbound"},
		}},
	}}
	var out []Finding
	checkNACLs(snap, &out)
	f := findOne(out, "may block return traffic")
	if f == nil || f.Resource != "acl-default-hardened" {
		t.Errorf("hardened default NACL should be flagged, got %+v", out)
	}
}

func TestSubnetEgressViaOtherTargetsNotFlagged(t *testing.T) {
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{
			{ID: "subnet-tgw", CIDR: "10.0.0.0/24", AvailableIPs: 200},
			{ID: "subnet-isolated", CIDR: "10.0.1.0/24", AvailableIPs: 200},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-tgw", Associations: []string{"subnet-tgw"}, Routes: []Route{
				{Destination: "0.0.0.0/0", Target: "tgw-123", State: "active"},
			}},
			{ID: "rtb-isolated", Associations: []string{"subnet-isolated"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
			}},
		},
	}
	var out []Finding
	checkSubnets(snap, &out)

	for _, f := range out {
		if f.Resource == "subnet-tgw" && strings.Contains(f.Title, "no outbound internet path") {
			t.Errorf("centralized egress via a transit gateway should not be flagged: %+v", f)
		}
	}
	f := findOne(out, "no outbound internet path")
	if f == nil || f.Resource != "subnet-isolated" {
		t.Errorf("expected isolated subnet flagged, got %+v", out)
	}
}

func TestMapPublicIPNeedsIPv4DefaultRoute(t *testing.T) {
	// An ::/0 → igw- route does not make auto-assigned IPv4 public IPs usable.
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{{ID: "subnet-v6", CIDR: "10.0.0.0/24", AvailableIPs: 200, MapPublicIPOnLaunch: true}},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-v6", Associations: []string{"subnet-v6"}, Routes: []Route{
				{Destination: "::/0", Target: "igw-1", State: "active"},
			}},
		},
	}
	var out []Finding
	checkSubnets(snap, &out)
	if findOne(out, "auto-assigns public IPs") == nil {
		t.Error("IPv6-only IGW route should still flag the IPv4 map-public-ip mismatch")
	}
}

func TestTraceSameSubnetSkipsNACLs(t *testing.T) {
	// Two ENIs in one subnet behind a deny-all NACL: NACLs apply only at the
	// subnet boundary, so the path must be evaluated by SGs alone.
	snap := vpcSnapshot{
		VPCID: "vpc-1",
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-a", PrivateIP: "10.0.0.10", SubnetID: "subnet-1", SecurityGroups: []string{"sg-open"}},
			{ID: "eni-b", PrivateIP: "10.0.0.20", SubnetID: "subnet-1", SecurityGroups: []string{"sg-open"}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-open", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "All", PortRange: "All", Source: "10.0.0.0/16"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-1", Associations: []string{"subnet-1"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
			}},
		},
		NetworkACLs: []NACLInfo{
			{ID: "acl-denyall", Associations: []string{"subnet-1"}, Rules: []NACLRule{
				{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound"},
				{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Outbound"},
			}},
		},
	}
	res := tracePath(snap, traceRequest{SourceENIID: "eni-a", DestIP: "10.0.0.20", Protocol: "tcp", Port: 8080})
	if !res.Reachable {
		t.Fatalf("same-subnet traffic must not be blocked by the NACL: %+v", res.Hops)
	}
	for _, h := range res.Hops {
		if strings.Contains(h.Name, "NACL") && h.Status != hopNote {
			t.Errorf("NACL hops should be skipped notes for same-subnet traffic, got %+v", h)
		}
	}
}

func TestTraceInternetBlockedByStatelessReturn(t *testing.T) {
	snap := baseSnap()
	// Replace the permissive NACL on the public subnet with one that allows
	// all egress but only inbound 443 — replies to outbound connections land
	// on ephemeral ports and are dropped.
	snap.NetworkACLs = []NACLInfo{
		{ID: "acl-noreturn", Associations: []string{"subnet-pub"}, Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
			{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		}},
	}
	res := tracePath(snap, traceRequest{SourceENIID: "eni-web", DestIP: "internet", Protocol: "tcp", Port: 443})
	if res.Reachable {
		t.Fatal("expected the stateless return check to block the internet path")
	}
	if !strings.Contains(res.Summary, "NACL return") {
		t.Errorf("expected a NACL-return block, got %q", res.Summary)
	}
}

func TestTraceNATGatewayUnhealthy(t *testing.T) {
	snap := baseSnap()
	snap.NatGateways = []NatGWInfo{{ID: "nat-1", State: "failed"}}
	res := tracePath(snap, traceRequest{SourceENIID: "eni-db", DestIP: "internet", Protocol: "tcp", Port: 443})
	if res.Reachable {
		t.Fatal("a failed NAT gateway on the path must block the trace")
	}
	if !strings.Contains(res.Summary, "NAT gateway") {
		t.Errorf("expected NAT gateway block, got %q", res.Summary)
	}
}

func TestPortMatchFullNumericRangeAsAnyPort(t *testing.T) {
	if !portMatch("0-65535", -1) {
		t.Error("a 0-65535 rule should satisfy an any-port request")
	}
	if !portMatch("1-65535", -1) {
		t.Error("a 1-65535 rule should satisfy an any-port request")
	}
	if portMatch("443", -1) {
		t.Error("a single-port rule must not satisfy an any-port request")
	}
	if portMatch("1024-65535", -1) {
		t.Error("a partial range must not satisfy an any-port request")
	}
}

func TestFindENIBySecondaryIP(t *testing.T) {
	snap := vpcSnapshot{NetworkInterfaces: []ENIInfo{
		{ID: "eni-multi", PrivateIP: "10.0.0.10", SecondaryIPs: []string{"10.0.0.11", "10.0.0.12"}},
	}}
	if e := findENIByIP(snap, "10.0.0.11"); e == nil || e.ID != "eni-multi" {
		t.Errorf("secondary IP should resolve to its ENI, got %+v", e)
	}
	if e := findENIByIP(snap, "10.0.0.99"); e != nil {
		t.Errorf("unknown IP should not match, got %+v", e)
	}
}

func TestExposureReportCorrelation(t *testing.T) {
	snap := vpcSnapshot{
		Subnets: []SubnetInfo{
			{ID: "subnet-pub", CIDR: "10.0.0.0/24"},
			{ID: "subnet-priv", CIDR: "10.0.1.0/24"},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-pub", Associations: []string{"subnet-pub"}, Routes: []Route{
				{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"},
			}},
			{ID: "rtb-priv", Associations: []string{"subnet-priv"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
			}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-open", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
			}},
			{ID: "sg-closed", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/16"},
			}},
		},
		NetworkInterfaces: []ENIInfo{
			// Public IP + IGW route + open SG: actually reachable.
			{ID: "eni-exposed", PrivateIP: "10.0.0.10", PublicIP: "52.1.1.1", SubnetID: "subnet-pub", SecurityGroups: []string{"sg-open"}},
			// Public IP but no IGW route: not reachable.
			{ID: "eni-unrouted", PrivateIP: "10.0.1.10", PublicIP: "52.1.1.2", SubnetID: "subnet-priv", SecurityGroups: []string{"sg-open"}},
			// Public IP + IGW route but closed SG: not reachable.
			{ID: "eni-closed", PrivateIP: "10.0.0.11", PublicIP: "52.1.1.3", SubnetID: "subnet-pub", SecurityGroups: []string{"sg-closed"}},
		},
	}
	groups := exposureReport(snap)
	var reachable *xrefGroup
	for i := range groups {
		if strings.Contains(groups[i].Label, "Internet-reachable") {
			reachable = &groups[i]
		}
	}
	if reachable == nil {
		t.Fatalf("expected an internet-reachable correlation group, got %+v", groups)
	}
	if len(reachable.Items) != 1 || !strings.Contains(reachable.Items[0], "eni-exposed") {
		t.Errorf("only eni-exposed should be correlated as reachable, got %v", reachable.Items)
	}
	if !strings.Contains(reachable.Items[0], "HTTPS (TCP 443)") {
		t.Errorf("the reachable entry should list the exposed ports, got %q", reachable.Items[0])
	}
}

func TestPeeringSecondaryCIDROverlap(t *testing.T) {
	snap := vpcSnapshot{Peerings: []PeeringInfo{{
		ID:             "pcx-secondary",
		Status:         "active",
		RequesterCIDR:  "10.0.0.0/16",
		RequesterCIDRs: []string{"10.0.0.0/16", "10.50.0.0/16"},
		AccepterCIDR:   "172.16.0.0/16",
		AccepterCIDRs:  []string{"172.16.0.0/16", "10.50.1.0/24"},
	}}}
	var out []Finding
	checkPeerings(snap, &out)
	f := findOne(out, "overlapping CIDRs")
	if f == nil || f.Resource != "pcx-secondary" {
		t.Fatalf("expected overlap via secondary CIDRs, got %+v", out)
	}
	if !strings.Contains(f.Detail, "10.50.0.0/16") || !strings.Contains(f.Detail, "10.50.1.0/24") {
		t.Errorf("detail should name the overlapping pair, got %q", f.Detail)
	}
}

func TestNACLRuleQuota(t *testing.T) {
	build := func(n int) NACLInfo {
		nacl := NACLInfo{ID: "acl-big"}
		for i := 0; i < n; i++ {
			nacl.Rules = append(nacl.Rules, NACLRule{
				RuleNumber: int32(100 + i), Protocol: "TCP", PortRange: "443",
				CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound",
			})
		}
		// The immutable default rule must not count.
		nacl.Rules = append(nacl.Rules, NACLRule{
			RuleNumber: 32767, Protocol: "All", PortRange: "All",
			CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound",
		})
		return nacl
	}

	var out []Finding
	checkQuotas(vpcSnapshot{NetworkACLs: []NACLInfo{build(20)}}, &out)
	f := findOne(out, "Network ACL is approaching its rule limit")
	if f == nil || f.Severity != SevCritical {
		t.Errorf("20 rules should be critical, got %+v", out)
	}

	out = nil
	checkQuotas(vpcSnapshot{NetworkACLs: []NACLInfo{build(16)}}, &out)
	f = findOne(out, "Network ACL is approaching its rule limit")
	if f == nil || f.Severity != SevWarning {
		t.Errorf("16 rules should warn, got %+v", out)
	}

	out = nil
	checkQuotas(vpcSnapshot{NetworkACLs: []NACLInfo{build(5)}}, &out)
	if findOne(out, "Network ACL is approaching its rule limit") != nil {
		t.Error("5 rules should not be flagged")
	}
}

func TestUsesCustomDNSLinkLocalResolver(t *testing.T) {
	if usesCustomDNS([]string{"169.254.169.253"}) {
		t.Error("the link-local Amazon resolver is not a custom DNS server")
	}
	if !usesCustomDNS([]string{"169.254.169.253", "8.8.8.8"}) {
		t.Error("a real custom server should still be detected")
	}
}

func TestDiffDetectsNACLReassociation(t *testing.T) {
	old := vpcSnapshot{NetworkACLs: []NACLInfo{
		{ID: "acl-1", Associations: []string{"subnet-a"}},
	}}
	neu := vpcSnapshot{NetworkACLs: []NACLInfo{
		{ID: "acl-1", Associations: []string{"subnet-b"}},
	}}
	changes := diffSnapshots(old, neu)
	found := false
	for _, c := range changes {
		if c.Type == "Network ACL" && c.Kind == changeModified {
			found = true
		}
	}
	if !found {
		t.Errorf("a NACL re-association should appear in the diff, got %+v", changes)
	}
}

func TestDiffDetectsEndpointSGChange(t *testing.T) {
	old := vpcSnapshot{Endpoints: []EndpointInfo{
		{ID: "vpce-1", State: "available", SecurityGroups: []string{"sg-a"}},
	}}
	neu := vpcSnapshot{Endpoints: []EndpointInfo{
		{ID: "vpce-1", State: "available", SecurityGroups: []string{"sg-b"}},
	}}
	changes := diffSnapshots(old, neu)
	found := false
	for _, c := range changes {
		if c.Type == "VPC endpoint" && c.Kind == changeModified {
			found = true
		}
	}
	if !found {
		t.Errorf("an endpoint SG swap should appear in the diff, got %+v", changes)
	}
}
