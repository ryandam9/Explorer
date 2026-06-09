package vpctui

import (
	"strings"
	"testing"
)

func effSnap() vpcSnapshot {
	return vpcSnapshot{
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-app", SubnetID: "subnet-1", SecurityGroups: []string{"sg-a", "sg-b"}},
			{ID: "eni-bare", SubnetID: "subnet-1", SecurityGroups: []string{"sg-a"}},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-a", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
				{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/8"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
			{ID: "sg-b", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"}, // dup of sg-a
				{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "sg-a"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"}, // dup
			}},
		},
		NetworkACLs: []NACLInfo{
			{ID: "acl-1", Associations: []string{"subnet-1"}},
		},
	}
}

func TestComputeEffectiveRulesMergesAndDedups(t *testing.T) {
	res := computeEffectiveRules(effSnap(), "eni-app")
	if !res.Found {
		t.Fatal("expected ENI to be found")
	}
	// Inbound union: 443 (from both), 22, 3306 = 3 distinct rules.
	if len(res.Inbound) != 3 {
		t.Fatalf("expected 3 distinct inbound rules, got %d: %+v", len(res.Inbound), res.Inbound)
	}
	// Outbound union: All/All from both groups, deduped to 1.
	if len(res.Outbound) != 1 {
		t.Fatalf("expected 1 deduped outbound rule, got %d", len(res.Outbound))
	}

	// The 443 rule should list both contributing groups.
	var found bool
	for _, mr := range res.Inbound {
		if mr.Rule.PortRange == "443" {
			found = true
			if len(mr.SGs) != 2 || mr.SGs[0] != "sg-a" || mr.SGs[1] != "sg-b" {
				t.Errorf("443 rule should be contributed by sg-a and sg-b, got %v", mr.SGs)
			}
		}
	}
	if !found {
		t.Error("expected a 443 inbound rule")
	}

	if res.NACLID != "acl-1" {
		t.Errorf("expected acl-1 as the applicable NACL, got %q", res.NACLID)
	}
}

func TestComputeEffectiveRulesSingleGroup(t *testing.T) {
	res := computeEffectiveRules(effSnap(), "eni-bare")
	if len(res.Inbound) != 2 || len(res.Outbound) != 1 {
		t.Errorf("expected 2 inbound / 1 outbound for sg-a only, got %d/%d", len(res.Inbound), len(res.Outbound))
	}
	for _, mr := range res.Inbound {
		if len(mr.SGs) != 1 || mr.SGs[0] != "sg-a" {
			t.Errorf("single-group rules should list only sg-a, got %v", mr.SGs)
		}
	}
}

func TestComputeEffectiveRulesUnknownENI(t *testing.T) {
	res := computeEffectiveRules(effSnap(), "eni-nope")
	if res.Found {
		t.Error("expected Found=false for unknown ENI")
	}
}

func TestRuleKeyStability(t *testing.T) {
	a := SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"}
	b := SGRule{Direction: "inbound", Protocol: "tcp", PortRange: "443", Source: "0.0.0.0/0"}
	if ruleKey(a) != ruleKey(b) {
		t.Errorf("ruleKey should be case-insensitive: %q vs %q", ruleKey(a), ruleKey(b))
	}
	c := SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "80", Source: "0.0.0.0/0"}
	if ruleKey(a) == ruleKey(c) {
		t.Error("different ports should produce different keys")
	}
}

func TestEffectiveRulesExplained(t *testing.T) {
	// Ensure the merged rules feed cleanly into the plain-English explainer.
	res := computeEffectiveRules(effSnap(), "eni-app")
	var sawHTTPS bool
	for _, mr := range res.Inbound {
		if strings.Contains(explainSGRule(mr.Rule), "HTTPS (TCP 443)") {
			sawHTTPS = true
		}
	}
	if !sawHTTPS {
		t.Error("expected the 443 rule to explain as HTTPS")
	}
}
