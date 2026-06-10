package vpctui

import (
	"strings"
	"testing"
	"time"
)

func exportSnap() fullExport {
	return fullExport{
		VPC: VPCInfo{
			ID:     "vpc-1",
			Name:   "primary",
			Region: "ap-southeast-2",
			CIDR:   "10.0.0.0/16",
			State:  "available",
			Tags:   map[string]string{"env": "prod"},
		},
		Snap: vpcSnapshot{
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
		},
		EC2: []EC2InstanceInfo{
			{ID: "i-1", Name: "app", State: "running", Type: "t3.micro", PrivateIP: "10.0.0.5"},
		},
	}
}

func TestExportMarkdownStructure(t *testing.T) {
	data := exportSnap()
	findings := analyzeVPC(data.Snap)
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	md := exportMarkdown(data, findings, at)

	for _, want := range []string{
		"# VPC Report: vpc-1 (ap-southeast-2)",
		"_Generated 2026-06-09 12:00:00 UTC_",
		"## VPC",
		"| CIDR | 10.0.0.0/16 |",
		"| env | prod |", // VPC tags rendered
		"## Summary",
		"| Subnets | 1 |",
		"| Security groups | 1 |",
		"| EC2 instances | 1 |",
		"## Findings (",
		"### Critical", // sg-web exposes SSH to the internet
		"sg-web",
		"## Subnets (1)",
		"### subnet-1",
		"| CIDR | 10.0.0.0/24 |",
		"| Available IPs | 200 |",
		"## Security groups (1)",
		"### sg-web — web",
		"**Inbound rules**",
		"| TCP | 22 | 0.0.0.0/0 | - |",
		"## Route tables (1)",
		"**Routes**",
		"| 0.0.0.0/0 | igw-1 | active |",
		"## Network interfaces (1)",
		"### eni-1",
		"| Attached to | i-1 |",
		"## EC2 instances (1)",
		"### i-1 — app",
		"| Type | t3.micro |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export markdown missing %q", want)
		}
	}
}

func TestExportMarkdownCleanVPC(t *testing.T) {
	// A VPC with no findings shows the clean-bill line and omits empty tables.
	data := fullExport{VPC: VPCInfo{ID: "vpc-empty"}, Snap: vpcSnapshot{VPCID: "vpc-empty"}}
	md := exportMarkdown(data, nil, time.Now())
	if !strings.Contains(md, "No issues detected") {
		t.Error("expected clean-bill finding line")
	}
	if strings.Contains(md, "## Subnets") {
		t.Error("empty subnet table should be omitted")
	}
	if strings.Contains(md, "VPC Report: vpc-empty (") {
		t.Error("title should omit the region parenthesis when region is empty")
	}
}

func TestWriteExportRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	data := exportSnap()
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	path, err := writeExport(data, analyzeVPC(data.Snap), at)
	if err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	if !strings.HasSuffix(path, "vpc-1-20260609-120000.md") {
		t.Errorf("unexpected export path: %s", path)
	}
}
