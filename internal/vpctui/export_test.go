package vpctui

import (
	"os"
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
		"_Generated 2026-06-09 22:00:00 AEST_", // 12:00 UTC shown in Melbourne time
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
		// Every resource type renders as a single table with a leading "#" column.
		"## Subnets (1)",
		"| # | ID | Name | CIDR | IPv6 CIDRs | AZ | Available IPs | State | Public | Default for AZ | Auto-assign public IP | Tags |",
		"| 1 | subnet-1 | - | 10.0.0.0/24 | - | a | 200 | - | Yes | No | No | - |",
		"## Security groups (1)",
		"| # | ID | Name | Description | VPC ID | Inbound rules | Outbound rules | Tags |",
		"| 1 | sg-web | web |", // rules packed into a cell
		"TCP 22 0.0.0.0/0",
		"## Route tables (1)",
		"0.0.0.0/0 → igw-1 (active)",
		"## Network interfaces (1)",
		"| # | ID | Description | Type | Status | Private IP | Public IP | Subnet ID | VPC ID | Availability zone | Attached to | Security groups | Source/dest check | Tags |",
		"| 1 | eni-1 |",
		"## EC2 instances (1)",
		"| 1 | i-1 | app | running | t3.micro |",
		// Workload services.
		"| ECS services | 1 |",
		"## ECS services (1)",
		"| 1 | api | prod | ACTIVE | FARGATE |",
		"| EKS clusters | 1 |",
		"## EKS clusters (1)",
		"| 1 | eks-prod | ACTIVE | 1.29 |",
		"| ElastiCache clusters | 1 |",
		"## ElastiCache clusters (1)",
		"| 1 | cache-1 | redis 7.1 |",
		"| Redshift clusters | 1 |",
		"## Redshift clusters (1)",
		"| 1 | rs-1 | available |",
		"| EFS file systems | 1 |",
		"## EFS file systems (1)",
		"| 1 | fs-1 | shared |",
		"| EMR clusters | 1 |",
		"## EMR clusters (1)",
		"| 1 | j-1 | spark | WAITING | subnet-1 |",
		// VPN / transit gateway.
		"| VPN gateways | 1 |",
		"## VPN gateways (1)",
		"| 1 | vgw-1 | available | ipsec.1 | 64512 |",
		"## VPN connections (1)",
		"| 1 | vpn-1 | available | ipsec.1 |",
		"## Customer gateways (1)",
		"| 1 | cgw-1 | available | ipsec.1 | 203.0.113.10 |",
		"## Transit gateway attachments (1)",
		"| 1 | tgw-attach-1 | tgw-1 | available | vpc | vpc-1 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export markdown missing %q", want)
		}
	}
}

func TestExportHTMLStructure(t *testing.T) {
	data := exportSnap()
	findings := analyzeVPC(data.Snap)
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	html := exportHTML(data, findings, at)

	for _, want := range []string{
		"<!doctype html>",
		"<title>VPC report \u00b7 vpc-1</title>",
		`<b class="mono">vpc-1</b>`,
		"Region <b>ap-southeast-2</b>",
		"generated 2026-06-09 22:00:00 AEST",
		`<nav class="toc"`,
		`<a href="#subnets-1">Subnets<span class="n">1</span></a>`, // count split out of the link text
		`<div class="tbl">`,                                        // every table in its own scrolling box
		`<div class="kpis">`,                                       // headline counts under the header
		`<section id="subnets-1">`,                                 // headings wrapped into sections
		`<section class="arch" id="architecture">`,
		`<div class="layer-toggles"`,
		`id="lt-sg"`,
		`id="lt-routing"`, // the new relationship layers are switchable
		`id="lt-endpoints"`,
		`id="lt-peering"`,
		`.arch:has(#lt-traffic:not(:checked))`, // pure-CSS layer toggle (no JS)
		`<g data-layer="traffic">`,             // the inline SVG carries the layers
		`prefers-color-scheme: dark`,           // the report has a real dark mode
		`data-theme="dark"`,
		`id="themebtn"`,
		`class="dgtools"`, // the diagram's zoom / reset controls
		`data-act="reset"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("export HTML missing %q", want)
		}
	}

	// The report is one self-contained file: no stylesheet, script or font is
	// fetched over the network, so it reads the same offline and years later.
	for _, forbidden := range []string{
		"cdn.datatables.net", "code.jquery.com", "fonts.googleapis.com", "<link ",
	} {
		if strings.Contains(html, forbidden) {
			t.Errorf("export HTML reaches the network: found %q", forbidden)
		}
	}
}

// A long table gets a filter box; a short one does not, since it is all on
// screen already and the box would just be furniture.
func TestExportHTMLFiltersOnlyLongTables(t *testing.T) {
	long := strings.Repeat("<tr>\n<td>x</td>\n</tr>\n", filterMinRows)
	got := wrapTables("<table>\n<tbody>\n" + long + "</tbody>\n</table>")
	if !strings.Contains(got, `class="filter"`) {
		t.Errorf("a %d-row table got no filter box", filterMinRows)
	}
	if !strings.Contains(got, plural(filterMinRows, "row", "rows")) {
		t.Errorf("row count missing from the filter bar:\n%s", got)
	}

	short := strings.Repeat("<tr>\n<td>x</td>\n</tr>\n", filterMinRows-1)
	got = wrapTables("<table>\n<tbody>\n" + short + "</tbody>\n</table>")
	if strings.Contains(got, `class="filter"`) {
		t.Error("a short table should not get a filter box")
	}
	if !strings.Contains(got, `<div class="tbl">`) {
		t.Error("every table still goes in a scroll box")
	}
}

// Content before the first heading (the intro line, the partial-data notice)
// must survive sectionizing rather than being swallowed with the first section.
func TestSectionizeKeepsPreamble(t *testing.T) {
	in := `<p>intro</p>` + "\n" + `<h2 id="a">A</h2>` + "\n<p>body</p>\n"
	got := sectionize(in)
	if !strings.Contains(got, "<p>intro</p>") {
		t.Errorf("preamble lost:\n%s", got)
	}
	if !strings.Contains(got, `<section id="a">`) || !strings.Contains(got, "<p>body</p>") {
		t.Errorf("section not built:\n%s", got)
	}
	if strings.Count(got, "<section") != strings.Count(got, "</section>") {
		t.Errorf("unbalanced sections:\n%s", got)
	}
}

// A report with no headings at all (an empty VPC) must still render its tables
// rather than losing the content to a missing-section path.
func TestSectionizeWithoutHeadings(t *testing.T) {
	got := sectionize("<p>only a paragraph</p>")
	if !strings.Contains(got, "only a paragraph") {
		t.Errorf("content dropped when there are no headings: %q", got)
	}
}

func TestSanitizedAnchorNameMatchesHeadings(t *testing.T) {
	cases := map[string]string{
		"Subnets (1)":         "subnets-1",
		"VPC endpoints (2)":   "vpc-endpoints-2",
		"Security groups (1)": "security-groups-1",
	}
	for in, want := range cases {
		if got := sanitizedAnchorName(in); got != want {
			t.Errorf("sanitizedAnchorName(%q) = %q, want %q", in, got, want)
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

// A failed resource listing must be flagged in the report, so an empty section
// isn't mistaken for "none present".
func TestExportMarkdownPartialData(t *testing.T) {
	data := fullExport{
		VPC:        VPCInfo{ID: "vpc-x"},
		Snap:       vpcSnapshot{VPCID: "vpc-x"},
		LoadErrors: []string{"rds", "lambda"},
	}
	md := exportMarkdown(data, nil, time.Now())
	if !strings.Contains(md, "Partial data") || !strings.Contains(md, "rds, lambda") {
		t.Errorf("expected a partial-data warning naming the failed listings:\n%s", md)
	}
}

func TestWriteExportRoundTrip(t *testing.T) {
	tempHome(t)
	data := exportSnap()
	at := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	mdPath, htmlPath, svgPath, err := writeExport(data, analyzeVPC(data.Snap), at)
	if err != nil {
		t.Fatalf("writeExport: %v", err)
	}
	if !strings.HasSuffix(mdPath, "vpc-1-20260609-220000.md") { // 12:00 UTC → 22:00 Melbourne (AEST)
		t.Errorf("unexpected markdown export path: %s", mdPath)
	}
	if !strings.HasSuffix(htmlPath, "vpc-1-20260609-220000.html") {
		t.Errorf("unexpected html export path: %s", htmlPath)
	}
	if !strings.HasSuffix(svgPath, "vpc-1-20260609-220000.svg") {
		t.Errorf("unexpected svg export path: %s", svgPath)
	}
	if b, rerr := os.ReadFile(svgPath); rerr != nil || !strings.Contains(string(b), "<svg") {
		t.Errorf("svg file missing or not an SVG: err=%v", rerr)
	}
}

// Table cells must not wrap. Every AWS identifier contains a hyphen, which the
// browser treats as a break opportunity, so a narrow column split
// "subnet-08eb40f52431d2921" across two lines — unreadable, and impossible to
// scan down a column. The table scrolls instead, and the row stays identifiable
// while it does.
func TestReportTablesDoNotWrapCells(t *testing.T) {
	data := exportSnap()
	html := exportHTML(data, analyzeVPC(data.Snap), time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"th,td{text-align:left;padding:9px 12px;border-bottom:1px solid var(--line);vertical-align:top;white-space:nowrap}",
		"min-width:max-content", // the table may grow past its box…
		".tbl{overflow:auto",    // …which then scrolls
		"::-webkit-scrollbar",   // with a visible bar
		"scrollbar-width:thin",
		".tbl.pinned td:nth-child(2)", // the ID column pins beside the counter
		"--col1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("report stylesheet missing %q", want)
		}
	}
	if !strings.Contains(html, "pinIDColumns") {
		t.Error("the script does not measure the counter column, so the pinned ID would be misaligned")
	}
}

// The report is a document of identifiers and literals, so the Markdown
// renderer must not "improve" its punctuation. Blackfriday enables
// Smartypants by default, which typeset "122.104.83.64/32" as a fraction —
// superscript 64 over subscript 32 — and does two other things just as bad.
func TestReportDoesNotTypesetIdentifiers(t *testing.T) {
	data := exportSnap()
	data.Snap.SecurityGroups = []SGInfo{{
		ID: "sg-1", Name: "web", VPCID: "vpc-1",
		Rules: []SGRule{
			{Direction: "inbound", Protocol: "TCP", PortRange: "22", Source: "122.104.83.64/32"},
			{Direction: "inbound", Protocol: "TCP", PortRange: "443", Source: "10.0.0.0/8",
				Description: `run with --profile "prod"`},
		},
	}}
	html := exportHTML(data, analyzeVPC(data.Snap), time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC))

	if !strings.Contains(html, "122.104.83.64/32") {
		t.Error("the CIDR was rewritten; it must survive verbatim")
	}
	for _, bad := range []string{
		"<sup>64</sup>", "&frasl;", "<sub>32</sub>", // fractions
		"&ndash;", "&mdash;", // dashes: --profile must stay copy-pastable
		"&ldquo;", "&rdquo;", // curly quotes: ARNs and policy JSON must paste
	} {
		if strings.Contains(html, bad) {
			t.Errorf("report contains %q — Smartypants is rewriting the content", bad)
		}
	}
	if !strings.Contains(html, "--profile") {
		t.Error("a command flag was rewritten into a dash")
	}
}
