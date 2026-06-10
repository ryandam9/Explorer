package vpctui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
	b.WriteString("_Generated " + generatedAt.UTC().Format("2006-01-02 15:04:05 UTC") + "_\n\n")

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
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Subnets (%d)\n\n", len(items)))
	for _, s := range items {
		mdHeading(b, s.ID, s.Name)
		mdKV(b, [][2]string{
			{"ID", s.ID},
			{"Name", s.Name},
			{"VPC ID", s.VPCID},
			{"CIDR", s.CIDR},
			{"IPv6 CIDRs", strings.Join(s.Ipv6CIDRs, ", ")},
			{"Availability zone", s.AZ},
			{"Available IPs", itoa(int(s.AvailableIPs))},
			{"State", s.State},
			{"Public", boolStr(s.IsPublic)},
			{"Default for AZ", boolStr(s.DefaultForAz)},
			{"Auto-assign public IP", boolStr(s.MapPublicIPOnLaunch)},
		})
		mdTags(b, s.Tags)
	}
}

func writeSecurityGroups(b *strings.Builder, items []SGInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Security groups (%d)\n\n", len(items)))
	for _, sg := range items {
		mdHeading(b, sg.ID, sg.Name)
		mdKV(b, [][2]string{
			{"ID", sg.ID},
			{"Name", sg.Name},
			{"Description", sg.Description},
			{"VPC ID", sg.VPCID},
		})
		writeSGRules(b, "Inbound rules", "Inbound", sg.Rules)
		writeSGRules(b, "Outbound rules", "Outbound", sg.Rules)
		mdTags(b, sg.Tags)
	}
}

func writeSGRules(b *strings.Builder, label, dir string, rules []SGRule) {
	var group []SGRule
	for _, r := range rules {
		if strings.EqualFold(r.Direction, dir) {
			group = append(group, r)
		}
	}
	if len(group) == 0 {
		return
	}
	b.WriteString("**" + label + "**\n\n")
	b.WriteString("| Protocol | Ports | Source/Dest | Description |\n|---|---|---|---|\n")
	for _, r := range group {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
			mdCell(r.Protocol), mdCell(r.PortRange), mdCell(r.Source), mdCell(orDash(r.Description))))
	}
	b.WriteString("\n")
}

func writeRouteTables(b *strings.Builder, items []RouteTableInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Route tables (%d)\n\n", len(items)))
	for _, rt := range items {
		mdHeading(b, rt.ID, rt.Name)
		mdKV(b, [][2]string{
			{"ID", rt.ID},
			{"Name", rt.Name},
			{"VPC ID", rt.VPCID},
			{"Main", boolStr(rt.IsMain)},
			{"Associated subnets", strings.Join(rt.Associations, ", ")},
		})
		if len(rt.Routes) > 0 {
			b.WriteString("**Routes**\n\n| Destination | Target | State |\n|---|---|---|\n")
			for _, r := range rt.Routes {
				b.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
					mdCell(r.Destination), mdCell(r.Target), mdCell(r.State)))
			}
			b.WriteString("\n")
		}
		mdTags(b, rt.Tags)
	}
}

func writeInternetGateways(b *strings.Builder, items []IGWInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Internet gateways (%d)\n\n", len(items)))
	for _, igw := range items {
		mdHeading(b, igw.ID, igw.Name)
		mdKV(b, [][2]string{
			{"ID", igw.ID},
			{"Name", igw.Name},
			{"State", igw.State},
			{"VPC ID", igw.VPCID},
		})
		mdTags(b, igw.Tags)
	}
}

func writeNatGateways(b *strings.Builder, items []NatGWInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## NAT gateways (%d)\n\n", len(items)))
	for _, n := range items {
		mdHeading(b, n.ID, n.Name)
		mdKV(b, [][2]string{
			{"ID", n.ID},
			{"Name", n.Name},
			{"Type", n.Type},
			{"State", n.State},
			{"Subnet ID", n.SubnetID},
			{"VPC ID", n.VPCID},
			{"Public IP", n.PublicIP},
			{"Private IP", n.PrivateIP},
		})
		mdTags(b, n.Tags)
	}
}

func writeNACLs(b *strings.Builder, items []NACLInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Network ACLs (%d)\n\n", len(items)))
	for _, nacl := range items {
		mdHeading(b, nacl.ID, nacl.Name)
		mdKV(b, [][2]string{
			{"ID", nacl.ID},
			{"Name", nacl.Name},
			{"VPC ID", nacl.VPCID},
			{"Default", boolStr(nacl.IsDefault)},
			{"Associated subnets", strings.Join(nacl.Associations, ", ")},
		})
		writeNACLRules(b, "Inbound rules", "Inbound", nacl.Rules)
		writeNACLRules(b, "Outbound rules", "Outbound", nacl.Rules)
		mdTags(b, nacl.Tags)
	}
}

func writeNACLRules(b *strings.Builder, label, dir string, rules []NACLRule) {
	var group []NACLRule
	for _, r := range rules {
		if strings.EqualFold(r.Direction, dir) {
			group = append(group, r)
		}
	}
	if len(group) == 0 {
		return
	}
	sort.SliceStable(group, func(i, j int) bool { return group[i].RuleNumber < group[j].RuleNumber })
	b.WriteString("**" + label + "**\n\n")
	b.WriteString("| Rule # | Protocol | Ports | CIDR | Action |\n|---|---|---|---|---|\n")
	for _, r := range group {
		b.WriteString(fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
			r.RuleNumber, mdCell(r.Protocol), mdCell(r.PortRange), mdCell(r.CIDR), mdCell(r.Action)))
	}
	b.WriteString("\n")
}

func writeEndpoints(b *strings.Builder, items []EndpointInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## VPC endpoints (%d)\n\n", len(items)))
	for _, e := range items {
		mdHeading(b, e.ID, e.ServiceName)
		mdKV(b, [][2]string{
			{"ID", e.ID},
			{"Service", e.ServiceName},
			{"Type", e.Type},
			{"State", e.State},
			{"VPC ID", e.VPCID},
			{"Route tables", strings.Join(e.RouteTableIDs, ", ")},
			{"Subnets", strings.Join(e.SubnetIDs, ", ")},
			{"Security groups", strings.Join(e.SecurityGroups, ", ")},
			{"Private DNS", boolStr(e.PrivateDNSEnabled)},
		})
		mdTags(b, e.Tags)
	}
}

func writePeerings(b *strings.Builder, items []PeeringInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Peering connections (%d)\n\n", len(items)))
	for _, p := range items {
		mdHeading(b, p.ID, "")
		mdKV(b, [][2]string{
			{"ID", p.ID},
			{"Status", p.Status},
			{"Requester VPC", p.RequesterVPCID},
			{"Requester region", p.RequesterRegion},
			{"Requester CIDR", p.RequesterCIDR},
			{"Accepter VPC", p.AccepterVPCID},
			{"Accepter region", p.AccepterRegion},
			{"Accepter CIDR", p.AccepterCIDR},
		})
		mdTags(b, p.Tags)
	}
}

func writeFlowLogs(b *strings.Builder, items []FlowLogInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Flow logs (%d)\n\n", len(items)))
	for _, fl := range items {
		mdHeading(b, fl.ID, "")
		mdKV(b, [][2]string{
			{"ID", fl.ID},
			{"Resource ID", fl.ResourceID},
			{"Traffic type", fl.TrafficType},
			{"Status", fl.Status},
			{"Destination", fl.LogDestination},
			{"Log format", fl.LogFormat},
		})
		mdTags(b, fl.Tags)
	}
}

func writeNetworkInterfaces(b *strings.Builder, items []ENIInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Network interfaces (%d)\n\n", len(items)))
	for _, e := range items {
		mdHeading(b, e.ID, e.Description)
		mdKV(b, [][2]string{
			{"ID", e.ID},
			{"Description", e.Description},
			{"Type", e.Type},
			{"Status", e.Status},
			{"Private IP", e.PrivateIP},
			{"Public IP", e.PublicIP},
			{"Subnet ID", e.SubnetID},
			{"VPC ID", e.VPCID},
			{"Availability zone", e.AZ},
			{"Attached to", e.AttachedTo},
			{"Security groups", strings.Join(e.SecurityGroups, ", ")},
			{"Source/dest check", boolStr(e.SourceDestCheck)},
		})
		mdTags(b, e.Tags)
	}
}

func writeEC2(b *strings.Builder, items []EC2InstanceInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## EC2 instances (%d)\n\n", len(items)))
	for _, in := range items {
		mdHeading(b, in.ID, in.Name)
		mdKV(b, [][2]string{
			{"ID", in.ID},
			{"Name", in.Name},
			{"State", in.State},
			{"Type", in.Type},
			{"Private IP", in.PrivateIP},
			{"Public IP", in.PublicIP},
			{"VPC ID", in.VPCID},
			{"Subnet ID", in.SubnetID},
			{"Availability zone", in.AZ},
			{"Platform", in.Platform},
			{"Launch time", in.LaunchTime},
			{"IAM role", in.IamRole},
			{"AMI ID", in.AMIID},
			{"Key pair", in.KeyPair},
		})
		mdTags(b, in.Tags)
	}
}

func writeLambdas(b *strings.Builder, items []LambdaFunctionInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Lambda functions (%d)\n\n", len(items)))
	for _, fn := range items {
		mdHeading(b, fn.Name, "")
		mdKV(b, [][2]string{
			{"Name", fn.Name},
			{"Runtime", fn.Runtime},
			{"State", fn.State},
			{"Handler", fn.Handler},
			{"Memory", fmt.Sprintf("%d MB", fn.MemoryMB)},
			{"Timeout", fmt.Sprintf("%ds", fn.TimeoutSec)},
			{"Last modified", fn.LastModified},
			{"VPC ID", fn.VPCID},
			{"Subnets", strings.Join(fn.SubnetIDs, ", ")},
			{"Security groups", strings.Join(fn.SGIDs, ", ")},
		})
	}
}

func writeRDS(b *strings.Builder, items []RDSInstanceInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## RDS instances (%d)\n\n", len(items)))
	for _, db := range items {
		mdHeading(b, db.ID, "")
		mdKV(b, [][2]string{
			{"ID", db.ID},
			{"Engine", db.Engine},
			{"Class", db.Class},
			{"Status", db.Status},
			{"Endpoint", db.Endpoint},
			{"VPC ID", db.VPCID},
			{"Availability zone", db.AZ},
			{"Multi-AZ", boolStr(db.MultiAZ)},
			{"Storage (GB)", itoa(int(db.Storage))},
		})
	}
}

func writeLoadBalancers(b *strings.Builder, items []LoadBalancerInfo) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("## Load balancers (%d)\n\n", len(items)))
	for _, lb := range items {
		mdHeading(b, lb.Name, "")
		mdKV(b, [][2]string{
			{"Name", lb.Name},
			{"ARN", lb.ARN},
			{"Type", lb.Type},
			{"Scheme", lb.Scheme},
			{"State", lb.State},
			{"DNS name", lb.DNSName},
			{"VPC ID", lb.VPCID},
			{"Created at", lb.CreatedAt},
		})
	}
}

// ---------------------------------------------------------------------------
// Markdown render helpers
// ---------------------------------------------------------------------------

// mdHeading writes a per-resource "### id — name" subsection heading. The name
// is omitted when empty.
func mdHeading(b *strings.Builder, id, name string) {
	if name != "" {
		b.WriteString("### " + id + " — " + name + "\n\n")
	} else {
		b.WriteString("### " + id + "\n\n")
	}
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

// writeExport writes the report to a timestamped file and returns its path.
func writeExport(data fullExport, findings []Finding, now time.Time) (string, error) {
	dir, err := exportDir()
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.md", data.VPC.ID, now.Format("20060102-150405"))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(exportMarkdown(data, findings, now)), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
