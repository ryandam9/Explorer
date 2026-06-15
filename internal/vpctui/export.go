package vpctui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // embed the zone database so Australia/Melbourne always resolves

	"github.com/ryandam9/aws_explorer/internal/csvexport"
)

// reportLoc is the timezone report timestamps are shown in. Melbourne observes
// AEST (UTC+10) and AEDT (UTC+11, during daylight saving); formatting with the
// "MST" layout token emits whichever abbreviation applies. Falls back to UTC if
// the zone can't be loaded.
var reportLoc = func() *time.Location {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// reportTime formats t for display in the report: Melbourne local time with the
// zone abbreviation, e.g. "2026-06-15 16:53:00 AEST".
func reportTime(t time.Time) string {
	return t.In(reportLoc).Format("2006-01-02 15:04:05 MST")
}

// ---------------------------------------------------------------------------
// Export for case notes
//
// exportMarkdown renders a VPC plus every resource in (and attached to) it as a
// self-contained Markdown report suitable for pasting into a support case or
// runbook. Unlike the on-screen summary it is exhaustive: every resource is
// listed with all of its fields, tags, and nested rules/routes. The rendering
// is pure (timestamp injected) so it is fully unit-testable.
// ---------------------------------------------------------------------------

// fullExport bundles everything needed to render the complete report: the VPC's
// own attributes, the networking snapshot analyzed by the findings engine, and
// the workload resources that live inside the VPC.
type fullExport struct {
	VPC      VPCInfo
	Snap     vpcSnapshot
	FlowLogs []FlowLogInfo
	EC2      []EC2InstanceInfo
	Lambdas  []LambdaFunctionInfo
	RDS      []RDSInstanceInfo
	LBs      []LoadBalancerInfo

	// Additional workload services running inside the VPC.
	ECSServices []ECSServiceInfo
	EKSClusters []EKSClusterInfo
	ElastiCache []ElastiCacheClusterInfo
	Redshift    []RedshiftClusterInfo
	EFS         []EFSFileSystemInfo
	EMR         []EMRClusterInfo

	// EC2-native VPN / transit-gateway connectivity.
	VPNGateways               []VPNGatewayInfo
	VPNConnections            []VPNConnectionInfo
	CustomerGateways          []CustomerGatewayInfo
	TransitGatewayAttachments []TransitGatewayAttachmentInfo
}

// exportMarkdown builds the complete Markdown report for a VPC.
func exportMarkdown(data fullExport, findings []Finding, generatedAt time.Time) string {
	var b strings.Builder
	snap := data.Snap
	region := data.VPC.Region

	title := "VPC Report: " + data.VPC.ID
	if region != "" {
		title += " (" + region + ")"
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("_Generated " + reportTime(generatedAt) + "_\n\n")

	// VPC attributes.
	writeVPCSection(&b, data.VPC)

	// Summary counts (every resource type, including workloads).
	b.WriteString("## Summary\n\n| Resource | Count |\n|---|---|\n")
	for _, row := range [][2]string{
		{"Subnets", itoa(len(snap.Subnets))},
		{"Security groups", itoa(len(snap.SecurityGroups))},
		{"Route tables", itoa(len(snap.RouteTables))},
		{"Internet gateways", itoa(len(snap.InternetGateways))},
		{"NAT gateways", itoa(len(snap.NatGateways))},
		{"Network ACLs", itoa(len(snap.NetworkACLs))},
		{"VPC endpoints", itoa(len(snap.Endpoints))},
		{"Peering connections", itoa(len(snap.Peerings))},
		{"Network interfaces", itoa(len(snap.NetworkInterfaces))},
		{"Flow logs", itoa(len(data.FlowLogs))},
		{"EC2 instances", itoa(len(data.EC2))},
		{"Lambda functions", itoa(len(data.Lambdas))},
		{"RDS instances", itoa(len(data.RDS))},
		{"Load balancers", itoa(len(data.LBs))},
		{"ECS services", itoa(len(data.ECSServices))},
		{"EKS clusters", itoa(len(data.EKSClusters))},
		{"ElastiCache clusters", itoa(len(data.ElastiCache))},
		{"Redshift clusters", itoa(len(data.Redshift))},
		{"EFS file systems", itoa(len(data.EFS))},
		{"EMR clusters", itoa(len(data.EMR))},
		{"VPN gateways", itoa(len(data.VPNGateways))},
		{"VPN connections", itoa(len(data.VPNConnections))},
		{"Customer gateways", itoa(len(data.CustomerGateways))},
		{"Transit gateway attachments", itoa(len(data.TransitGatewayAttachments))},
	} {
		b.WriteString("| " + row[0] + " | " + row[1] + " |\n")
	}
	b.WriteString("\n")

	// Findings, grouped by severity.
	crit, warn, info := countBySeverity(findings)
	b.WriteString(fmt.Sprintf("## Findings (%d critical, %d warning, %d info)\n\n", crit, warn, info))
	if len(findings) == 0 {
		b.WriteString("No issues detected. ✓\n\n")
	} else {
		writeFindingGroup(&b, "Critical", SevCritical, findings)
		writeFindingGroup(&b, "Warning", SevWarning, findings)
		writeFindingGroup(&b, "Info", SevInfo, findings)
	}

	// Full resource inventory — every resource with all of its fields.
	writeSubnets(&b, snap.Subnets)
	writeSecurityGroups(&b, snap.SecurityGroups)
	writeRouteTables(&b, snap.RouteTables)
	writeInternetGateways(&b, snap.InternetGateways)
	writeNatGateways(&b, snap.NatGateways)
	writeNACLs(&b, snap.NetworkACLs)
	writeEndpoints(&b, snap.Endpoints)
	writePeerings(&b, snap.Peerings)
	writeFlowLogs(&b, data.FlowLogs)
	writeNetworkInterfaces(&b, snap.NetworkInterfaces)
	writeEC2(&b, data.EC2)
	writeLambdas(&b, data.Lambdas)
	writeRDS(&b, data.RDS)
	writeLoadBalancers(&b, data.LBs)
	writeECSServices(&b, data.ECSServices)
	writeEKSClusters(&b, data.EKSClusters)
	writeElastiCache(&b, data.ElastiCache)
	writeRedshift(&b, data.Redshift)
	writeEFS(&b, data.EFS)
	writeEMR(&b, data.EMR)
	writeVPNGateways(&b, data.VPNGateways)
	writeVPNConnections(&b, data.VPNConnections)
	writeCustomerGateways(&b, data.CustomerGateways)
	writeTransitGatewayAttachments(&b, data.TransitGatewayAttachments)

	return b.String()
}

// ---------------------------------------------------------------------------
// Per-resource-type renderers
// ---------------------------------------------------------------------------

func writeVPCSection(b *strings.Builder, v VPCInfo) {
	b.WriteString("## VPC\n\n")
	mdKV(b, [][2]string{
		{"ID", v.ID},
		{"Name", v.Name},
		{"Region", v.Region},
		{"CIDR", v.CIDR},
		{"IPv6 CIDRs", strings.Join(v.Ipv6CIDRs, ", ")},
		{"State", v.State},
		{"Default VPC", boolStr(v.IsDefault)},
		{"Instance tenancy", v.InstanceTenancy},
		{"DHCP options", v.DhcpOptionsID},
		{"Owner ID", v.OwnerId},
	})
	mdTags(b, v.Tags)
}

func writeSubnets(b *strings.Builder, items []SubnetInfo) {
	rows := make([][]string, len(items))
	for i, s := range items {
		rows[i] = []string{
			s.ID, s.Name, s.CIDR, strings.Join(s.Ipv6CIDRs, ", "), s.AZ,
			itoa(int(s.AvailableIPs)), s.State, boolStr(s.IsPublic),
			boolStr(s.DefaultForAz), boolStr(s.MapPublicIPOnLaunch), tagsList(s.Tags),
		}
	}
	writeTable(b, "Subnets", []string{
		"ID", "Name", "CIDR", "IPv6 CIDRs", "AZ", "Available IPs", "State",
		"Public", "Default for AZ", "Auto-assign public IP", "Tags",
	}, rows)
}

func writeSecurityGroups(b *strings.Builder, items []SGInfo) {
	rows := make([][]string, len(items))
	for i, sg := range items {
		rows[i] = []string{
			sg.ID, sg.Name, sg.Description, sg.VPCID,
			sgRulesCell(sg.Rules, "Inbound"), sgRulesCell(sg.Rules, "Outbound"),
			tagsList(sg.Tags),
		}
	}
	writeTable(b, "Security groups", []string{
		"ID", "Name", "Description", "VPC ID", "Inbound rules", "Outbound rules", "Tags",
	}, rows)
}

func writeRouteTables(b *strings.Builder, items []RouteTableInfo) {
	rows := make([][]string, len(items))
	for i, rt := range items {
		rows[i] = []string{
			rt.ID, rt.Name, rt.VPCID, boolStr(rt.IsMain),
			listCell(rt.Associations), routesCell(rt.Routes), tagsList(rt.Tags),
		}
	}
	writeTable(b, "Route tables", []string{
		"ID", "Name", "VPC ID", "Main", "Associated subnets", "Routes", "Tags",
	}, rows)
}

func writeInternetGateways(b *strings.Builder, items []IGWInfo) {
	rows := make([][]string, len(items))
	for i, igw := range items {
		rows[i] = []string{igw.ID, igw.Name, igw.State, igw.VPCID, tagsList(igw.Tags)}
	}
	writeTable(b, "Internet gateways", []string{"ID", "Name", "State", "VPC ID", "Tags"}, rows)
}

func writeNatGateways(b *strings.Builder, items []NatGWInfo) {
	rows := make([][]string, len(items))
	for i, n := range items {
		rows[i] = []string{
			n.ID, n.Name, n.Type, n.State, n.SubnetID, n.VPCID,
			n.PublicIP, n.PrivateIP, tagsList(n.Tags),
		}
	}
	writeTable(b, "NAT gateways", []string{
		"ID", "Name", "Type", "State", "Subnet ID", "VPC ID", "Public IP", "Private IP", "Tags",
	}, rows)
}

func writeNACLs(b *strings.Builder, items []NACLInfo) {
	rows := make([][]string, len(items))
	for i, nacl := range items {
		rows[i] = []string{
			nacl.ID, nacl.Name, nacl.VPCID, boolStr(nacl.IsDefault),
			listCell(nacl.Associations), naclRulesCell(nacl.Rules, "Inbound"),
			naclRulesCell(nacl.Rules, "Outbound"), tagsList(nacl.Tags),
		}
	}
	writeTable(b, "Network ACLs", []string{
		"ID", "Name", "VPC ID", "Default", "Associated subnets", "Inbound rules", "Outbound rules", "Tags",
	}, rows)
}

func writeEndpoints(b *strings.Builder, items []EndpointInfo) {
	rows := make([][]string, len(items))
	for i, e := range items {
		rows[i] = []string{
			e.ID, e.ServiceName, e.Type, e.State, e.VPCID,
			listCell(e.RouteTableIDs), listCell(e.SubnetIDs), listCell(e.SecurityGroups),
			boolStr(e.PrivateDNSEnabled), tagsList(e.Tags),
		}
	}
	writeTable(b, "VPC endpoints", []string{
		"ID", "Service", "Type", "State", "VPC ID", "Route tables", "Subnets",
		"Security groups", "Private DNS", "Tags",
	}, rows)
}

func writePeerings(b *strings.Builder, items []PeeringInfo) {
	rows := make([][]string, len(items))
	for i, p := range items {
		rows[i] = []string{
			p.ID, p.Status, p.RequesterVPCID, p.RequesterRegion, p.RequesterCIDR,
			p.AccepterVPCID, p.AccepterRegion, p.AccepterCIDR, tagsList(p.Tags),
		}
	}
	writeTable(b, "Peering connections", []string{
		"ID", "Status", "Requester VPC", "Requester region", "Requester CIDR",
		"Accepter VPC", "Accepter region", "Accepter CIDR", "Tags",
	}, rows)
}

func writeFlowLogs(b *strings.Builder, items []FlowLogInfo) {
	rows := make([][]string, len(items))
	for i, fl := range items {
		rows[i] = []string{
			fl.ID, fl.ResourceID, fl.TrafficType, fl.Status, fl.LogDestination,
			fl.LogFormat, tagsList(fl.Tags),
		}
	}
	writeTable(b, "Flow logs", []string{
		"ID", "Resource ID", "Traffic type", "Status", "Destination", "Log format", "Tags",
	}, rows)
}

func writeNetworkInterfaces(b *strings.Builder, items []ENIInfo) {
	rows := make([][]string, len(items))
	for i, e := range items {
		rows[i] = []string{
			e.ID, e.Description, e.Type, e.Status, e.PrivateIP, e.PublicIP,
			e.SubnetID, e.VPCID, e.AZ, e.AttachedTo, listCell(e.SecurityGroups),
			boolStr(e.SourceDestCheck), tagsList(e.Tags),
		}
	}
	writeTable(b, "Network interfaces", []string{
		"ID", "Description", "Type", "Status", "Private IP", "Public IP", "Subnet ID",
		"VPC ID", "Availability zone", "Attached to", "Security groups", "Source/dest check", "Tags",
	}, rows)
}

func writeEC2(b *strings.Builder, items []EC2InstanceInfo) {
	rows := make([][]string, len(items))
	for i, in := range items {
		rows[i] = []string{
			in.ID, in.Name, in.State, in.Type, in.PrivateIP, in.PublicIP, in.VPCID,
			in.SubnetID, in.AZ, in.Platform, in.LaunchTime, in.IamRole, in.AMIID,
			in.KeyPair, tagsList(in.Tags),
		}
	}
	writeTable(b, "EC2 instances", []string{
		"ID", "Name", "State", "Type", "Private IP", "Public IP", "VPC ID", "Subnet ID",
		"Availability zone", "Platform", "Launch time", "IAM role", "AMI ID", "Key pair", "Tags",
	}, rows)
}

func writeLambdas(b *strings.Builder, items []LambdaFunctionInfo) {
	rows := make([][]string, len(items))
	for i, fn := range items {
		rows[i] = []string{
			fn.Name, fn.Runtime, fn.State, fn.Handler,
			fmt.Sprintf("%d MB", fn.MemoryMB), fmt.Sprintf("%ds", fn.TimeoutSec),
			fn.LastModified, fn.VPCID, listCell(fn.SubnetIDs), listCell(fn.SGIDs),
		}
	}
	writeTable(b, "Lambda functions", []string{
		"Name", "Runtime", "State", "Handler", "Memory", "Timeout", "Last modified",
		"VPC ID", "Subnets", "Security groups",
	}, rows)
}

func writeRDS(b *strings.Builder, items []RDSInstanceInfo) {
	rows := make([][]string, len(items))
	for i, db := range items {
		rows[i] = []string{
			db.ID, db.Engine, db.Class, db.Status, db.Endpoint, db.VPCID, db.AZ,
			boolStr(db.MultiAZ), itoa(int(db.Storage)),
		}
	}
	writeTable(b, "RDS instances", []string{
		"ID", "Engine", "Class", "Status", "Endpoint", "VPC ID", "Availability zone",
		"Multi-AZ", "Storage (GB)",
	}, rows)
}

func writeLoadBalancers(b *strings.Builder, items []LoadBalancerInfo) {
	rows := make([][]string, len(items))
	for i, lb := range items {
		rows[i] = []string{
			lb.Name, lb.ARN, lb.Type, lb.Scheme, lb.State, lb.DNSName, lb.VPCID, lb.CreatedAt,
		}
	}
	writeTable(b, "Load balancers", []string{
		"Name", "ARN", "Type", "Scheme", "State", "DNS name", "VPC ID", "Created at",
	}, rows)
}

func writeECSServices(b *strings.Builder, items []ECSServiceInfo) {
	rows := make([][]string, len(items))
	for i, s := range items {
		rows[i] = []string{
			s.Name, s.Cluster, s.Status, s.LaunchType,
			itoa(int(s.DesiredCount)), itoa(int(s.RunningCount)),
			listCell(s.SubnetIDs), listCell(s.SGIDs),
		}
	}
	writeTable(b, "ECS services", []string{
		"Name", "Cluster", "Status", "Launch type", "Desired count", "Running count",
		"Subnets", "Security groups",
	}, rows)
}

func writeEKSClusters(b *strings.Builder, items []EKSClusterInfo) {
	rows := make([][]string, len(items))
	for i, cl := range items {
		rows[i] = []string{
			cl.Name, cl.Status, cl.Version, cl.Endpoint, cl.VPCID,
			listCell(cl.SubnetIDs), listCell(cl.SecurityGroups),
		}
	}
	writeTable(b, "EKS clusters", []string{
		"Name", "Status", "Version", "Endpoint", "VPC ID", "Subnets", "Security groups",
	}, rows)
}

func writeElastiCache(b *strings.Builder, items []ElastiCacheClusterInfo) {
	rows := make([][]string, len(items))
	for i, cl := range items {
		rows[i] = []string{
			cl.ID, strings.TrimSpace(cl.Engine + " " + cl.EngineVersion), cl.Status,
			cl.NodeType, itoa(int(cl.NumNodes)), cl.SubnetGroup, cl.VPCID,
		}
	}
	writeTable(b, "ElastiCache clusters", []string{
		"ID", "Engine", "Status", "Node type", "Nodes", "Subnet group", "VPC ID",
	}, rows)
}

func writeRedshift(b *strings.Builder, items []RedshiftClusterInfo) {
	rows := make([][]string, len(items))
	for i, cl := range items {
		rows[i] = []string{
			cl.ID, cl.Status, cl.NodeType, itoa(int(cl.NumNodes)), cl.DBName,
			cl.Endpoint, cl.SubnetGroup, cl.VPCID,
		}
	}
	writeTable(b, "Redshift clusters", []string{
		"ID", "Status", "Node type", "Nodes", "Database", "Endpoint", "Subnet group", "VPC ID",
	}, rows)
}

func writeEFS(b *strings.Builder, items []EFSFileSystemInfo) {
	rows := make([][]string, len(items))
	for i, fs := range items {
		rows[i] = []string{
			fs.ID, fs.Name, fs.State, fs.PerformanceMode, boolStr(fs.Encrypted),
			itoa(fs.MountTargets), listCell(fs.SubnetIDs), fs.VPCID,
		}
	}
	writeTable(b, "EFS file systems", []string{
		"ID", "Name", "State", "Performance mode", "Encrypted", "Mount targets (this VPC)",
		"Subnets", "VPC ID",
	}, rows)
}

func writeEMR(b *strings.Builder, items []EMRClusterInfo) {
	rows := make([][]string, len(items))
	for i, cl := range items {
		rows[i] = []string{cl.ID, cl.Name, cl.State, cl.SubnetID}
	}
	writeTable(b, "EMR clusters", []string{"ID", "Name", "State", "Subnet ID"}, rows)
}

func writeVPNGateways(b *strings.Builder, items []VPNGatewayInfo) {
	rows := make([][]string, len(items))
	for i, g := range items {
		rows[i] = []string{g.ID, g.State, g.Type, g.AmazonSideASN, tagsList(g.Tags)}
	}
	writeTable(b, "VPN gateways", []string{"ID", "State", "Type", "Amazon side ASN", "Tags"}, rows)
}

func writeVPNConnections(b *strings.Builder, items []VPNConnectionInfo) {
	rows := make([][]string, len(items))
	for i, v := range items {
		rows[i] = []string{
			v.ID, v.State, v.Type, v.Category, v.CustomerGatewayID,
			v.VPNGatewayID, v.TransitGatewayID, tagsList(v.Tags),
		}
	}
	writeTable(b, "VPN connections", []string{
		"ID", "State", "Type", "Category", "Customer gateway", "VPN gateway", "Transit gateway", "Tags",
	}, rows)
}

func writeCustomerGateways(b *strings.Builder, items []CustomerGatewayInfo) {
	rows := make([][]string, len(items))
	for i, g := range items {
		rows[i] = []string{g.ID, g.State, g.Type, g.IPAddress, g.BgpAsn, tagsList(g.Tags)}
	}
	writeTable(b, "Customer gateways", []string{
		"ID", "State", "Type", "IP address", "BGP ASN", "Tags",
	}, rows)
}

func writeTransitGatewayAttachments(b *strings.Builder, items []TransitGatewayAttachmentInfo) {
	rows := make([][]string, len(items))
	for i, a := range items {
		rows[i] = []string{
			a.ID, a.TransitGatewayID, a.State, a.ResourceType, a.ResourceID, tagsList(a.Tags),
		}
	}
	writeTable(b, "Transit gateway attachments", []string{
		"ID", "Transit gateway", "State", "Resource type", "Resource ID", "Tags",
	}, rows)
}

// ---------------------------------------------------------------------------
// Markdown render helpers
// ---------------------------------------------------------------------------

// writeTable renders a whole resource section as one table: a leading "#"
// sequence column (the per-section ordinal), the given headers, and one row per
// item. Cells are escaped; empty cells become "-". The section header carries
// the count. An empty section renders nothing.
func writeTable(b *strings.Builder, title string, headers []string, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## %s (%d)\n\n", title, len(rows)))
	b.WriteString("| # | " + strings.Join(headers, " | ") + " |\n")
	sep := "| --- |"
	for range headers {
		sep += " --- |"
	}
	b.WriteString(sep + "\n")
	for i, r := range rows {
		cells := make([]string, len(r))
		for j, c := range r {
			cells[j] = mdCell(orDash(c))
		}
		b.WriteString(fmt.Sprintf("| %d | %s |\n", i+1, strings.Join(cells, " | ")))
	}
	b.WriteString("\n")
}

// tagsList renders tags as a sorted "key = value" list joined by <br> so the
// HTML report stacks them inside a single cell. Empty when there are no tags.
func tagsList(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+" = "+tags[k])
	}
	return strings.Join(parts, "<br>")
}

// listCell joins multiple IDs/values into one cell, one per line (via <br>).
func listCell(items []string) string {
	return strings.Join(items, "<br>")
}

// sgRulesCell renders a security group's rules for one direction as a stacked
// "proto ports source (description)" list inside a single cell.
func sgRulesCell(rules []SGRule, dir string) string {
	var lines []string
	for _, r := range rules {
		if !strings.EqualFold(r.Direction, dir) {
			continue
		}
		line := fmt.Sprintf("%s %s %s", r.Protocol, r.PortRange, r.Source)
		if r.Description != "" {
			line += " (" + r.Description + ")"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "<br>")
}

// naclRulesCell renders a network ACL's rules for one direction as a stacked
// "rule#: proto ports cidr action" list, sorted by rule number.
func naclRulesCell(rules []NACLRule, dir string) string {
	var group []NACLRule
	for _, r := range rules {
		if strings.EqualFold(r.Direction, dir) {
			group = append(group, r)
		}
	}
	sort.SliceStable(group, func(i, j int) bool { return group[i].RuleNumber < group[j].RuleNumber })
	lines := make([]string, 0, len(group))
	for _, r := range group {
		lines = append(lines, fmt.Sprintf("%d: %s %s %s %s", r.RuleNumber, r.Protocol, r.PortRange, r.CIDR, r.Action))
	}
	return strings.Join(lines, "<br>")
}

// routesCell renders a route table's routes as a stacked "dest → target (state)"
// list inside a single cell.
func routesCell(routes []Route) string {
	lines := make([]string, 0, len(routes))
	for _, r := range routes {
		lines = append(lines, fmt.Sprintf("%s → %s (%s)", r.Destination, r.Target, r.State))
	}
	return strings.Join(lines, "<br>")
}

// mdKV writes a two-column Field/Value table, dashing out empty values.
func mdKV(b *strings.Builder, rows [][2]string) {
	b.WriteString("| Field | Value |\n|---|---|\n")
	for _, r := range rows {
		b.WriteString("| " + r[0] + " | " + mdCell(orDash(r[1])) + " |\n")
	}
	b.WriteString("\n")
}

// mdTags writes a sorted Key/Value tag table, or nothing when there are none.
func mdTags(b *strings.Builder, tags map[string]string) {
	if len(tags) == 0 {
		return
	}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("**Tags**\n\n| Key | Value |\n|---|---|\n")
	for _, k := range keys {
		b.WriteString("| " + mdCell(k) + " | " + mdCell(tags[k]) + " |\n")
	}
	b.WriteString("\n")
}

// mdCell escapes characters that would otherwise break a Markdown table cell.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "|", "\\|")
}

func writeFindingGroup(b *strings.Builder, label string, sev Severity, findings []Finding) {
	var group []Finding
	for _, f := range findings {
		if f.Severity == sev {
			group = append(group, f)
		}
	}
	if len(group) == 0 {
		return
	}
	b.WriteString("### " + label + "\n\n")
	for _, f := range group {
		b.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s\n", f.Title, f.Resource, f.Detail))
		if f.Fix != "" {
			b.WriteString("  - Fix: " + f.Fix + "\n")
		}
	}
	b.WriteString("\n")
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// ---------------------------------------------------------------------------
// File output
// ---------------------------------------------------------------------------

// exportDir returns the directory where reports are written, creating it.
func exportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".aws_explorer", "exports")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// writeExport writes the Markdown and HTML reports to timestamped files sharing
// a basename and returns both paths.
func writeExport(data fullExport, findings []Finding, now time.Time) (mdPath, htmlPath string, err error) {
	dir, err := exportDir()
	if err != nil {
		return "", "", err
	}
	base := fmt.Sprintf("%s-%s", data.VPC.ID, now.In(reportLoc).Format("20060102-150405"))
	mdPath = filepath.Join(dir, base+".md")
	if err := os.WriteFile(mdPath, []byte(exportMarkdown(data, findings, now)), 0o644); err != nil {
		return "", "", err
	}
	htmlPath = filepath.Join(dir, base+".html")
	if err := os.WriteFile(htmlPath, []byte(exportHTML(data, findings, now)), 0o644); err != nil {
		return mdPath, "", err
	}
	return mdPath, htmlPath, nil
}

// exportResourceCSV writes the currently displayed resource table (full
// column set, current filter and display order) to a timestamped CSV and
// returns its path. An empty path with nil error means there was nothing to
// export.
func (m *Model) exportResourceCSV() (string, error) {
	maps := m.resourceView
	if len(maps) == 0 || m.selectedVPC == nil {
		return "", nil
	}
	fields := m.colFields(m.activeResource)
	header := make([]string, 0, len(fields))
	for _, f := range fields {
		header = append(header, f.Title)
	}
	rows := make([][]string, 0, len(maps))
	for _, r := range maps {
		row := make([]string, 0, len(fields))
		for _, f := range fields {
			row = append(row, r[f.Key])
		}
		rows = append(rows, row)
	}
	dir, err := csvexport.DefaultDir()
	if err != nil {
		return "", err
	}
	return csvexport.Write(dir, m.selectedVPC.ID+"-"+rtKey(m.activeResource), header, rows)
}
