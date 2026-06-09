package vpctui

import (
	"strings"
	"testing"
)

func TestExplainSGRule(t *testing.T) {
	cases := []struct {
		name string
		rule SGRule
		want []string // substrings that must all be present
		deny []string // substrings that must NOT be present
	}{
		{
			name: "https from anywhere",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
			want: []string{"Allow inbound", "HTTPS (TCP 443)", "from", "anywhere on the internet (0.0.0.0/0)"},
			deny: []string{"⚠"}, // HTTPS to the world is normal, not flagged
		},
		{
			name: "ssh from anywhere is risky",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"},
			want: []string{"SSH (TCP 22)", "⚠ remote admin access open to the entire internet"},
		},
		{
			name: "mysql from anywhere is risky",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "3306", Source: "0.0.0.0/0"},
			want: []string{"MySQL/Aurora (TCP 3306)", "⚠ database/cache port exposed to the entire internet"},
		},
		{
			name: "all traffic from anywhere",
			rule: SGRule{Direction: "Inbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			want: []string{"all traffic", "⚠ ALL ports open to the entire internet"},
		},
		{
			name: "from another security group",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "5432", Source: "sg-0abc123"},
			want: []string{"PostgreSQL (TCP 5432)", "resources in security group sg-0abc123"},
			deny: []string{"⚠"}, // SG-to-SG is not public
		},
		{
			name: "private cidr single host",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.1.5/32"},
			want: []string{"SSH (TCP 22)", "the single host 10.0.1.5"},
			deny: []string{"⚠"},
		},
		{
			name: "private network range",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "8080", Source: "172.16.0.0/16"},
			want: []string{"HTTP (alt) (TCP 8080)", "the private network 172.16.0.0/16"},
			deny: []string{"⚠"},
		},
		{
			name: "outbound all to anywhere",
			rule: SGRule{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			want: []string{"Allow outbound", "all traffic", "to", "anywhere on the internet"},
		},
		{
			name: "udp port range",
			rule: SGRule{Direction: "Inbound", Protocol: "UDP", PortRange: "1024-2048", Source: "10.0.0.0/8"},
			want: []string{"UDP ports 1024-2048", "the private network 10.0.0.0/8"},
		},
		{
			name: "port range spanning ssh from internet is risky",
			rule: SGRule{Direction: "Inbound", Protocol: "TCP", PortRange: "0-1024", Source: "0.0.0.0/0"},
			want: []string{"⚠ port range exposes sensitive ports to the entire internet"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := explainSGRule(tc.rule)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("explainSGRule = %q\n  missing substring %q", got, w)
				}
			}
			for _, d := range tc.deny {
				if strings.Contains(got, d) {
					t.Errorf("explainSGRule = %q\n  should not contain %q", got, d)
				}
			}
		})
	}
}

func TestIsPrivateCIDR(t *testing.T) {
	priv := []string{"10.0.0.0/8", "10.1.2.3/32", "172.16.5.0/24", "172.31.0.0/16", "192.168.1.0/24"}
	pub := []string{"172.15.0.0/16", "172.32.0.0/16", "8.8.8.8/32", "0.0.0.0/0", "11.0.0.0/8"}
	for _, c := range priv {
		if !isPrivateCIDR(c) {
			t.Errorf("isPrivateCIDR(%q) = false, want true", c)
		}
	}
	for _, c := range pub {
		if isPrivateCIDR(c) {
			t.Errorf("isPrivateCIDR(%q) = true, want false", c)
		}
	}
}

func TestEncodeSGRulesIncludesExplanations(t *testing.T) {
	out := encodeSGRules([]SGRule{
		{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"},
	})
	if !strings.Contains(out, "In plain English:") {
		t.Error("expected an explanation section header")
	}
	if !strings.Contains(out, "SSH (TCP 22)") || !strings.Contains(out, "⚠") {
		t.Errorf("explanation section missing humanized rule:\n%s", out)
	}
}
