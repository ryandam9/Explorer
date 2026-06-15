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
		ECSServices: []ECSServiceInfo{
			{Cluster: "prod", Name: "api", Status: "ACTIVE", LaunchType: "FARGATE", DesiredCount: 2, RunningCount: 2, SubnetIDs: []string{"subnet-1"}},
		},
		EKSClusters: []EKSClusterInfo{
			{Name: "eks-prod", Status: "ACTIVE", Version: "1.29", VPCID: "vpc-1", SubnetIDs: []string{"subnet-1"}},
		},
		ElastiCache: []ElastiCacheClusterInfo{
			{ID: "cache-1", Engine: "redis", EngineVersion: "7.1", Status: "available", NodeType: "cache.t3.micro", NumNodes: 1, SubnetGroup: "cache-subnets", VPCID: "vpc-1"},
		},
		Redshift: []RedshiftClusterInfo{
			{ID: "rs-1", Status: "available", NodeType: "ra3.xlplus", NumNodes: 2, DBName: "analytics", Endpoint: "rs-1.abc.redshift.amazonaws.com:5439", SubnetGroup: "rs-subnets", VPCID: "vpc-1"},
		},
		EFS: []EFSFileSystemInfo{
			{ID: "fs-1", Name: "shared", State: "available", PerformanceMode: "generalPurpose", Encrypted: true, MountTargets: 1, SubnetIDs: []string{"subnet-1"}, VPCID: "vpc-1"},
		},
		EMR: []EMRClusterInfo{
			{ID: "j-1", Name: "spark", State: "WAITING", SubnetID: "subnet-1"},
		},
		VPNGateways: []VPNGatewayInfo{
			{ID: "vgw-1", State: "available", Type: "ipsec.1", AmazonSideASN: "64512"},
		},
		VPNConnections: []VPNConnectionInfo{
			{ID: "vpn-1", State: "available", Type: "ipsec.1", CustomerGatewayID: "cgw-1", VPNGatewayID: "vgw-1"},
		},
		CustomerGateways: []CustomerGatewayInfo{
			{ID: "cgw-1", State: "available", Type: "ipsec.1", IPAddress: "203.0.113.10", BgpAsn: "65000"},
		},
		TransitGatewayAttachments: []TransitGatewayAttachmentInfo{
			{ID: "tgw-attach-1", TransitGatewayID: "tgw-1", State: "available", ResourceType: "vpc", ResourceID: "vpc-1"},
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
		// Workload services.
		"| ECS services | 1 |",
		"## ECS services (1)",
		"### api — prod",
		"| Launch type | FARGATE |",
		"| EKS clusters | 1 |",
		"## EKS clusters (1)",
		"### eks-prod",
		"| Version | 1.29 |",
		"| ElastiCache clusters | 1 |",
		"## ElastiCache clusters (1)",
		"### cache-1",
		"| Engine | redis 7.1 |",
		"| Redshift clusters | 1 |",
		"## Redshift clusters (1)",
		"### rs-1",
		"| Database | analytics |",
		"| EFS file systems | 1 |",
		"## EFS file systems (1)",
		"### fs-1 — shared",
		"| Encrypted | Yes |",
		"| EMR clusters | 1 |",
		"## EMR clusters (1)",
		"### j-1 — spark",
		// VPN / transit gateway.
		"| VPN gateways | 1 |",
		"## VPN gateways (1)",
		"### vgw-1",
		"| Amazon side ASN | 64512 |",
		"## VPN connections (1)",
		"### vpn-1",
		"| Customer gateway | cgw-1 |",
		"## Customer gateways (1)",
		"### cgw-1",
		"| IP address | 203.0.113.10 |",
		"## Transit gateway attachments (1)",
		"### tgw-attach-1",
		"| Transit gateway | tgw-1 |",
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
