package vpctui

import (
	"strings"
	"testing"
)

func TestExplainNACLRule(t *testing.T) {
	cases := []struct {
		name string
		rule NACLRule
		want []string
		deny []string
	}{
		{
			name: "allow inbound https from anywhere",
			rule: NACLRule{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			want: []string{"Rule 100:", "Allow inbound", "HTTPS (TCP 443)", "from", "anywhere on the internet (0.0.0.0/0)"},
			deny: []string{"⚠"},
		},
		{
			name: "allow inbound ssh from anywhere is risky",
			rule: NACLRule{RuleNumber: 110, Protocol: "TCP", PortRange: "22", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
			want: []string{"Rule 110:", "Allow inbound", "SSH (TCP 22)", "⚠ remote admin access open to the entire internet"},
		},
		{
			name: "deny rule is not flagged even if sensitive+public",
			rule: NACLRule{RuleNumber: 90, Protocol: "TCP", PortRange: "22", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound"},
			want: []string{"Rule 90:", "Deny inbound", "SSH (TCP 22)"},
			deny: []string{"⚠"},
		},
		{
			name: "default catch-all deny",
			rule: NACLRule{RuleNumber: 32767, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound"},
			want: []string{"Rule * (default)", "Deny inbound", "all traffic", "anywhere on the internet"},
			deny: []string{"⚠"},
		},
		{
			name: "outbound allow all",
			rule: NACLRule{RuleNumber: 100, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Outbound"},
			want: []string{"Rule 100:", "Allow outbound", "all traffic", "to", "anywhere on the internet"},
		},
		{
			name: "allow from private range",
			rule: NACLRule{RuleNumber: 120, Protocol: "TCP", PortRange: "3306", CIDR: "10.0.0.0/16", Action: "allow", Direction: "Inbound"},
			want: []string{"MySQL/Aurora (TCP 3306)", "the private network 10.0.0.0/16"},
			deny: []string{"⚠"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := explainNACLRule(tc.rule)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("explainNACLRule = %q\n  missing %q", got, w)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(got, d) {
					t.Errorf("explainNACLRule = %q\n  should not contain %q", got, d)
				}
			}
		})
	}
}

func TestEncodeNACLExplanationsSection(t *testing.T) {
	in := []NACLRule{
		{RuleNumber: 100, Protocol: "TCP", PortRange: "443", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		{RuleNumber: 32767, Protocol: "All", PortRange: "All", CIDR: "0.0.0.0/0", Action: "deny", Direction: "Inbound"},
	}
	out := encodeNACLExplanations(in, nil)
	for _, want := range []string{
		"In plain English",
		"stateless",
		"Inbound:",
		"Outbound:",
		"Rule 100:",
		"Rule * (default)",
		"(no outbound rules)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("section missing %q:\n%s", want, out)
		}
	}
}

func TestNACLToMapIncludesExplanations(t *testing.T) {
	m := naclToMap(NACLInfo{
		ID: "acl-123", VPCID: "vpc-1",
		Rules: []NACLRule{
			{RuleNumber: 100, Protocol: "TCP", PortRange: "22", CIDR: "0.0.0.0/0", Action: "allow", Direction: "Inbound"},
		},
	})
	rl := m["rule_list"]
	if !strings.Contains(rl, "In plain English") || !strings.Contains(rl, "SSH (TCP 22)") || !strings.Contains(rl, "⚠") {
		t.Errorf("rule_list missing humanized NACL rule:\n%s", rl)
	}
}
