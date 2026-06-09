package vpctui

import (
	"strings"
	"testing"
	"time"
)

func exportSnap() vpcSnapshot {
	return vpcSnapshot{
		VPCID: "vpc-1",
		Subnets: []SubnetInfo{
			{ID: "subnet-1", CIDR: "10.0.0.0/24", AZ: "a", AvailableIPs: 200, IsPublic: true},
		},
		SecurityGroups: []SGInfo{
			{ID: "sg-web", Name: "web", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "0.0.0.0/0"},
				{Direction: "Outbound", Protocol: "All", PortRange: "All", Source: "0.0.0.0/0"},
			}},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-1", IsMain: true, Routes: []Route{{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"}}},
		},
		NetworkInterfaces: []ENIInfo{
			{ID: "eni-1", Type: "interface", PrivateIP: "10.0.0.5", AttachedTo: "i-1"},
		},
	}
}

func TestExportMarkdownStructure(t *testing.T) {
	snap := exportSnap()
	findings := analyzeVPC(snap)
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	md := exportMarkdown(snap, findings, "ap-southeast-2", at)

	for _, want := range []string{
		"# VPC Report: vpc-1 (ap-southeast-2)",
		"_Generated 2026-06-09 12:00:00 UTC_",
		"## Summary",
		"| Subnets | 1 |",
		"| Security groups | 1 |",
		"## Findings (",
		"### Critical", // sg-web exposes SSH to the internet
		"sg-web",
		"## Subnets",
		"| subnet-1 | 10.0.0.0/24 | a | 200 | Yes |",
		"## Security groups",
		"| sg-web | web | 1 | 1 |",
		"## Route tables",
		"## Network interfaces",
		"| eni-1 | interface | 10.0.0.5 | - | i-1 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export markdown missing %q", want)
		}
	}
}

func TestExportMarkdownCleanVPC(t *testing.T) {
	// A VPC with no findings shows the clean-bill line and omits empty tables.
	snap := vpcSnapshot{VPCID: "vpc-empty"}
	md := exportMarkdown(snap, nil, "", time.Now())
	if !strings.Contains(md, "No issues detected") {
		t.Error("expected clean-bill finding line")
	}
	if strings.Contains(md, "## Subnets") {
		t.Error("empty subnet table should be omitted")
	}
	if strings.Contains(md, "(") && strings.Contains(md, "VPC Report: vpc-empty (") {
		t.Error("title should omit the region parenthesis when region is empty")
	}
}

func TestWriteExportRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	snap := exportSnap()
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	path, err := writeExport(snap, analyzeVPC(snap), "ap-southeast-2", at)
	if err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	if !strings.HasSuffix(path, "vpc-1-20260609-120000.md") {
		t.Errorf("unexpected export path: %s", path)
	}
}
