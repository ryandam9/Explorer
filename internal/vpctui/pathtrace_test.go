package vpctui

import (
	"strings"
	"testing"
)

// baseSnap builds a small two-subnet VPC: a web ENI in a public subnet and a
// db ENI in a private subnet, with permissive-but-realistic rules.
func baseSnap() vpcSnapshot {
	return vpcSnapshot{
		VPCID: "vpc-1",
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-web", PrivateIP: "10.0.0.10", PublicIP: "52.1.1.1", SubnetID: "subnet-pub", SecurityGroups: []string{"sg-web"}},
			{ID: "eni-db", PrivateIP: "10.0.1.20", SubnetID: "subnet-priv", SecurityGroups: []string{"sg-db"}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-web", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
			{ID: "sg-db", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "sg-web"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-pub", Associations: []string{"subnet-pub"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
				{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"},
			}},
			{ID: "rtb-priv", Associations: []string{"subnet-priv"}, Routes: []Route{
				{Destination: "10.0.0.0/16", Target: "local", State: "active"},
				{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"},
			}},
		},
		// Default NACL allowing everything both ways.
		NetworkACLs: []NACLInfo{
			{ID: "acl-default", IsDefault: true, Rules: []NACLRule{
				{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
				{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
			}},
		},
	}
}

func TestTraceReachableWebToDB(t *testing.T) {
	res := tracePath(baseSnap(), traceRequest{SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306})
	if !res.Reachable {
		t.Fatalf("expected reachable, got blocked: %+v", res.Hops)
	}
	if !strings.Contains(res.Summary, "eni-web → eni-db") {
		t.Errorf("unexpected summary: %s", res.Summary)
	}
}

func TestTraceBlockedAtDestSG(t *testing.T) {
	// Wrong port: sg-db only allows 3306 from sg-web.
	res := tracePath(baseSnap(), traceRequest{SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 5432})
	if res.Reachable {
		t.Fatal("expected blocked at destination SG")
	}
	last := res.Hops[len(res.Hops)-1]
	if last.Status != hopFail || !strings.Contains(last.Name, "security group ingress") {
		t.Errorf("expected block at dest SG ingress, got %+v", last)
	}
}

func TestTraceBlockedAtSourceEgress(t *testing.T) {
	snap := baseSnap()
	// Restrict sg-web egress to only 443.
	snap.SecurityGroups[0].Rules[1] = SGRule{Direction: "Outbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"}
	res := tracePath(snap, traceRequest{SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306})
	if res.Reachable {
		t.Fatal("expected blocked at source egress")
	}
	if got := res.Summary; !strings.Contains(got, "Security group egress") {
		t.Errorf("expected egress block summary, got %q", got)
	}
}

func TestTraceBlockedByNACLDeny(t *testing.T) {
	snap := baseSnap()
	// Custom NACL on the private subnet that denies inbound 3306.
	snap.NetworkACLs = append(snap.NetworkACLs, NACLInfo{
		ID: "acl-priv", Associations: []string{"subnet-priv"},
		Rules: []NACLRule{
			{RuleNumber: 90, Protocol: "TCP", PortRange: "3306", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound"},
			{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
		},
	})
	res := tracePath(snap, traceRequest{SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306})
	if res.Reachable {
		t.Fatal("expected blocked by NACL deny")
	}
	last := res.Hops[len(res.Hops)-1]
	if !strings.Contains(last.Name, "NACL ingress") || !strings.Contains(last.Detail, "denies") {
		t.Errorf("expected NACL deny, got %+v", last)
	}
}

func TestTraceInternetViaNAT(t *testing.T) {
	res := tracePath(baseSnap(), traceRequest{SourceENIID: "eni-db", DestIP: "internet", Protocol: "tcp", Port: 443})
	if !res.Reachable {
		t.Fatalf("expected reachable via NAT, got %+v", res.Hops)
	}
	if !strings.Contains(res.Summary, "NAT gateway") {
		t.Errorf("expected NAT summary, got %q", res.Summary)
	}
}

func TestTraceInternetViaIGWNeedsPublicIP(t *testing.T) {
	snap := baseSnap()
	snap.NetworkInterfaces[0].PublicIP = "" // web ENI loses its public IP
	res := tracePath(snap, traceRequest{SourceENIID: "eni-web", DestIP: "internet", Protocol: "tcp", Port: 443})
	if res.Reachable {
		t.Fatal("expected blocked: IGW path needs a public IP")
	}
	if !strings.Contains(res.Summary, "Internet gateway") {
		t.Errorf("expected IGW block, got %q", res.Summary)
	}
}

func TestTraceInternetViaIGWWithPublicIP(t *testing.T) {
	res := tracePath(baseSnap(), traceRequest{SourceENIID: "eni-web", DestIP: "8.8.8.8", Protocol: "tcp", Port: 443})
	if !res.Reachable {
		t.Fatalf("expected reachable via IGW, got %+v", res.Hops)
	}
	if !strings.Contains(res.Summary, "internet gateway") {
		t.Errorf("expected IGW summary, got %q", res.Summary)
	}
}

func TestTraceBlackholeRoute(t *testing.T) {
	snap := baseSnap()
	snap.RouteTables[1].Routes[1] = Route{Destination: "0.0.0.0/0", Target: "nat-dead", State: "blackhole"}
	res := tracePath(snap, traceRequest{SourceENIID: "eni-db", DestIP: "internet", Protocol: "tcp", Port: 443})
	if res.Reachable {
		t.Fatal("expected blocked by blackhole route")
	}
	if !strings.Contains(res.Summary, "Route table") {
		t.Errorf("expected route block, got %q", res.Summary)
	}
}

func TestTraceStatelessReturnBlocked(t *testing.T) {
	snap := baseSnap()
	// Private subnet NACL allows inbound 3306 and outbound nothing (no ephemeral return).
	snap.NetworkACLs = append(snap.NetworkACLs, NACLInfo{
		ID: "acl-priv", Associations: []string{"subnet-priv"},
		Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "TCP", PortRange: "3306", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Outbound"},
		},
	})
	res := tracePath(snap, traceRequest{SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306})
	if res.Reachable {
		t.Fatal("expected blocked: stateless return traffic not allowed")
	}
	if !strings.Contains(res.Summary, "return") {
		t.Errorf("expected stateless-return block, got %q", res.Summary)
	}
}

func TestTraceUnknownSource(t *testing.T) {
	res := tracePath(baseSnap(), traceRequest{SourceENIID: "eni-nope", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306})
	if res.Reachable || !strings.Contains(res.Summary, "Source") {
		t.Errorf("expected source-not-found block, got %q", res.Summary)
	}
}

func TestPortAndProtoMatchers(t *testing.T) {
	if !portMatch("All", 443) || !portMatch("443", 443) || !portMatch("1024-65535", 32768) {
		t.Error("portMatch positive cases failed")
	}
	if portMatch("443", 22) || portMatch("80-100", 200) {
		t.Error("portMatch should not match out-of-range ports")
	}
	if !protoMatch("All", "tcp") || !protoMatch("TCP", "tcp") {
		t.Error("protoMatch positive cases failed")
	}
	if protoMatch("UDP", "tcp") {
		t.Error("protoMatch should reject mismatched protocols")
	}
}

func TestLongestPrefixRoute(t *testing.T) {
	rt := &RouteTableInfo{Routes: []Route{
		{Destination: "10.0.0.0/16", Target: "local", State: "active"},
		{Destination: "10.0.1.0/24", Target: "pcx-1", State: "active"},
		{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"},
	}}
	r, ok := longestPrefixRoute(rt, ipOf("10.0.1.5"))
	if !ok || r.Target != "pcx-1" {
		t.Errorf("expected most-specific pcx-1 route, got %+v (ok=%v)", r, ok)
	}
	r, _ = longestPrefixRoute(rt, ipOf("203.0.113.9"))
	if r.Target != "igw-1" {
		t.Errorf("expected default route igw-1, got %+v", r)
	}
}
