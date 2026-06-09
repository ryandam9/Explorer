package vpctui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/aws_explorer/internal/table"
)

// ---------------------------------------------------------------------------
// Resource types and sidebar
// ---------------------------------------------------------------------------

type resourceType int

const (
	rtSubnets resourceType = iota
	rtSecurityGroups
	rtRouteTables
	rtInternetGateways
	rtNatGateways
	rtEndpoints
	rtNetworkACLs
	rtPeering
	rtFlowLogs
	rtEC2Instances
	rtLambda
	rtRDS
	rtLoadBalancers
	rtCount
)

func rtLabel(rt resourceType) string {
	switch rt {
	case rtSubnets:
		return "Subnets"
	case rtSecurityGroups:
		return "Security Groups"
	case rtRouteTables:
		return "Route Tables"
	case rtInternetGateways:
		return "Internet Gateways"
	case rtNatGateways:
		return "NAT Gateways"
	case rtEndpoints:
		return "VPC Endpoints"
	case rtNetworkACLs:
		return "Network ACLs"
	case rtPeering:
		return "Peering"
	case rtFlowLogs:
		return "Flow Logs"
	case rtEC2Instances:
		return "EC2 Instances"
	case rtLambda:
		return "Lambda Functions"
	case rtRDS:
		return "RDS Instances"
	case rtLoadBalancers:
		return "Load Balancers"
	default:
		return "Unknown"
	}
}

type sidebarCategory struct {
	name  string
	types []resourceType
}

var sidebarCategories = []sidebarCategory{
	{"NETWORK", []resourceType{rtSubnets, rtSecurityGroups, rtRouteTables, rtInternetGateways, rtNatGateways, rtEndpoints, rtNetworkACLs, rtPeering, rtFlowLogs}},
	{"COMPUTE", []resourceType{rtEC2Instances, rtLambda}},
	{"SERVICES", []resourceType{rtRDS, rtLoadBalancers}},
}

type sidebarItem struct {
	isHeader bool
	label    string
	rt       resourceType
}

func buildSidebarItems() []sidebarItem {
	var items []sidebarItem
	for _, cat := range sidebarCategories {
		items = append(items, sidebarItem{isHeader: true, label: cat.name})
		for _, rt := range cat.types {
			items = append(items, sidebarItem{label: rtLabel(rt), rt: rt})
		}
	}
	return items
}

// firstSelectableIdx returns the index of the first non-header sidebar item.
func firstSelectableIdx(items []sidebarItem) int {
	for i, item := range items {
		if !item.isHeader {
			return i
		}
	}
	return 0
}

// nextSelectableIdx returns the next non-header item index, wrapping around.
func nextSelectableIdx(items []sidebarItem, cur int, delta int) int {
	n := len(items)
	idx := cur
	for i := 0; i < n; i++ {
		idx = (idx + delta + n) % n
		if !items[idx].isHeader {
			return idx
		}
	}
	return cur
}

// ---------------------------------------------------------------------------
// Table columns per resource type
// ---------------------------------------------------------------------------

func columnsFor(rt resourceType) []table.Column {
	switch rt {
	case rtSubnets:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Subnet ID", Width: 24},
			{Title: "Name", Width: 18},
			{Title: "CIDR", Width: 16},
			{Title: "AZ", Width: 14},
			{Title: "Avail IPs", Width: 10},
			{Title: "Public", Width: 7},
			{Title: "State", Width: 10},
		}
	case rtSecurityGroups:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "SG ID", Width: 22},
			{Title: "Name", Width: 22},
			{Title: "In", Width: 5},
			{Title: "Out", Width: 5},
			{Title: "Description", Width: 36},
		}
	case rtRouteTables:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "RT ID", Width: 24},
			{Title: "Name", Width: 18},
			{Title: "Routes", Width: 7},
			{Title: "Subnets", Width: 7},
			{Title: "Main", Width: 6},
		}
	case rtInternetGateways:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "IGW ID", Width: 24},
			{Title: "Name", Width: 24},
			{Title: "State", Width: 12},
		}
	case rtNatGateways:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "NAT ID", Width: 24},
			{Title: "Name", Width: 18},
			{Title: "Type", Width: 8},
			{Title: "State", Width: 10},
			{Title: "Public IP", Width: 16},
			{Title: "Subnet", Width: 24},
		}
	case rtEndpoints:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Endpoint ID", Width: 24},
			{Title: "Service", Width: 40},
			{Title: "Type", Width: 12},
			{Title: "State", Width: 12},
		}
	case rtNetworkACLs:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "NACL ID", Width: 24},
			{Title: "Name", Width: 18},
			{Title: "Rules", Width: 6},
			{Title: "Subnets", Width: 7},
			{Title: "Default", Width: 8},
		}
	case rtPeering:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Peering ID", Width: 24},
			{Title: "Status", Width: 12},
			{Title: "Requester VPC", Width: 22},
			{Title: "Accepter VPC", Width: 22},
		}
	case rtFlowLogs:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Log ID", Width: 24},
			{Title: "Traffic", Width: 10},
			{Title: "Status", Width: 12},
			{Title: "Destination", Width: 40},
		}
	case rtEC2Instances:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Instance ID", Width: 20},
			{Title: "Name", Width: 18},
			{Title: "State", Width: 10},
			{Title: "Type", Width: 14},
			{Title: "Private IP", Width: 16},
			{Title: "Public IP", Width: 16},
			{Title: "AZ", Width: 14},
		}
	case rtLambda:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Function Name", Width: 30},
			{Title: "Runtime", Width: 14},
			{Title: "State", Width: 10},
			{Title: "Memory", Width: 8},
			{Title: "Timeout", Width: 9},
		}
	case rtRDS:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "DB ID", Width: 28},
			{Title: "Engine", Width: 20},
			{Title: "Class", Width: 16},
			{Title: "Status", Width: 12},
			{Title: "AZ", Width: 14},
			{Title: "Multi-AZ", Width: 9},
		}
	case rtLoadBalancers:
		return []table.Column{
			{Title: "#", Width: 4},
			{Title: "Name", Width: 24},
			{Title: "Type", Width: 12},
			{Title: "Scheme", Width: 12},
			{Title: "State", Width: 12},
			{Title: "DNS Name", Width: 40},
		}
	default:
		return []table.Column{{Title: "#", Width: 4}, {Title: "ID", Width: 40}}
	}
}

// ---------------------------------------------------------------------------
// Row and detail builders per resource type
// ---------------------------------------------------------------------------

type resourceData struct {
	rows    []table.Row
	details [][]string
}

func fetchResourceData(client *VPCClient, rt resourceType, vpcID string) (resourceData, error) {
	switch rt {
	case rtSubnets:
		items, err := client.ListSubnets(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, s := range items {
			pub := boolStr(s.IsPublic)
			rows = append(rows, table.Row{"", s.ID, orDash(s.Name), s.CIDR, s.AZ, fmt.Sprintf("%d", s.AvailableIPs), pub, s.State})
			details = append(details, subnetDetail(s))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtSecurityGroups:
		items, err := client.ListSecurityGroups(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, sg := range items {
			rows = append(rows, table.Row{"", sg.ID, sg.Name, fmt.Sprintf("%d", sg.InboundCount), fmt.Sprintf("%d", sg.OutboundCount), sg.Description})
			details = append(details, sgDetail(sg))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtRouteTables:
		items, err := client.ListRouteTables(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, rt := range items {
			rows = append(rows, table.Row{"", rt.ID, orDash(rt.Name), fmt.Sprintf("%d", len(rt.Routes)), fmt.Sprintf("%d", len(rt.Associations)), boolStr(rt.IsMain)})
			details = append(details, routeTableDetail(rt))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtInternetGateways:
		items, err := client.ListInternetGateways(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, igw := range items {
			rows = append(rows, table.Row{"", igw.ID, orDash(igw.Name), igw.State})
			details = append(details, igwDetail(igw))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtNatGateways:
		items, err := client.ListNatGateways(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, ngw := range items {
			rows = append(rows, table.Row{"", ngw.ID, orDash(ngw.Name), ngw.Type, ngw.State, orDash(ngw.PublicIP), ngw.SubnetID})
			details = append(details, natgwDetail(ngw))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtEndpoints:
		items, err := client.ListVPCEndpoints(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, ep := range items {
			rows = append(rows, table.Row{"", ep.ID, ep.ServiceName, ep.Type, ep.State})
			details = append(details, endpointDetail(ep))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtNetworkACLs:
		items, err := client.ListNetworkACLs(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, nacl := range items {
			rows = append(rows, table.Row{"", nacl.ID, orDash(nacl.Name), fmt.Sprintf("%d", len(nacl.Rules)), fmt.Sprintf("%d", len(nacl.Associations)), boolStr(nacl.IsDefault)})
			details = append(details, naclDetail(nacl))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtPeering:
		items, err := client.ListPeeringConnections(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, pc := range items {
			rows = append(rows, table.Row{"", pc.ID, pc.Status, pc.RequesterVPCID, pc.AccepterVPCID})
			details = append(details, peeringDetail(pc))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtFlowLogs:
		items, err := client.ListFlowLogs(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, fl := range items {
			rows = append(rows, table.Row{"", fl.ID, fl.TrafficType, fl.Status, fl.LogDestination})
			details = append(details, flowLogDetail(fl))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtEC2Instances:
		items, err := client.ListEC2Instances(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, inst := range items {
			rows = append(rows, table.Row{"", inst.ID, orDash(inst.Name), inst.State, inst.Type, orDash(inst.PrivateIP), orDash(inst.PublicIP), inst.AZ})
			details = append(details, ec2Detail(inst))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtLambda:
		items, err := client.ListLambdaFunctions(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, fn := range items {
			rows = append(rows, table.Row{"", fn.Name, fn.Runtime, fn.State, fmt.Sprintf("%d MB", fn.MemoryMB), fmt.Sprintf("%ds", fn.TimeoutSec)})
			details = append(details, lambdaDetail(fn))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtRDS:
		items, err := client.ListRDSInstances(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, db := range items {
			rows = append(rows, table.Row{"", db.ID, db.Engine, db.Class, db.Status, db.AZ, boolStr(db.MultiAZ)})
			details = append(details, rdsDetail(db))
		}
		return resourceData{seqRows(rows), details}, nil

	case rtLoadBalancers:
		items, err := client.ListLoadBalancers(vpcID)
		if err != nil {
			return resourceData{}, err
		}
		var rows []table.Row
		var details [][]string
		for _, lb := range items {
			rows = append(rows, table.Row{"", lb.Name, lb.Type, lb.Scheme, lb.State, lb.DNSName})
			details = append(details, lbDetail(lb))
		}
		return resourceData{seqRows(rows), details}, nil
	}
	return resourceData{}, nil
}

// ---------------------------------------------------------------------------
// Detail formatters
// ---------------------------------------------------------------------------

func dl(key, val string) string {
	return fmt.Sprintf("  %-22s %s", key, val)
}

func subnetDetail(s SubnetInfo) []string {
	lines := []string{
		dl("ID", s.ID),
		dl("Name", orDash(s.Name)),
		dl("CIDR", s.CIDR),
		dl("Availability Zone", s.AZ),
		dl("Available IPs", fmt.Sprintf("%d", s.AvailableIPs)),
		dl("Public (map-on-launch)", boolStr(s.MapPublicIPOnLaunch)),
		dl("Default for AZ", boolStr(s.DefaultForAz)),
		dl("State", s.State),
		dl("VPC ID", s.VPCID),
	}
	if len(s.Ipv6CIDRs) > 0 {
		lines = append(lines, dl("IPv6 CIDRs", strings.Join(s.Ipv6CIDRs, ", ")))
	}
	lines = append(lines, tagLines(s.Tags)...)
	return lines
}

func sgDetail(sg SGInfo) []string {
	lines := []string{
		dl("ID", sg.ID),
		dl("Name", sg.Name),
		dl("Description", sg.Description),
		dl("VPC ID", sg.VPCID),
		dl("Inbound rules", fmt.Sprintf("%d", sg.InboundCount)),
		dl("Outbound rules", fmt.Sprintf("%d", sg.OutboundCount)),
		"",
	}
	if len(sg.Rules) > 0 {
		lines = append(lines, "  Rules:")
		lines = append(lines, fmt.Sprintf("  %-10s %-8s %-12s %-22s %s", "Dir", "Proto", "Ports", "Source", "Description"))
		lines = append(lines, "  "+strings.Repeat("─", 70))
		for _, r := range sg.Rules {
			desc := r.Description
			if desc == "" {
				desc = "-"
			}
			lines = append(lines, fmt.Sprintf("  %-10s %-8s %-12s %-22s %s", r.Direction, r.Protocol, r.PortRange, r.Source, desc))
		}
	}
	lines = append(lines, tagLines(sg.Tags)...)
	return lines
}

func routeTableDetail(rt RouteTableInfo) []string {
	lines := []string{
		dl("ID", rt.ID),
		dl("Name", orDash(rt.Name)),
		dl("VPC ID", rt.VPCID),
		dl("Main", boolStr(rt.IsMain)),
		dl("Associated subnets", fmt.Sprintf("%d", len(rt.Associations))),
		"",
	}
	if len(rt.Associations) > 0 {
		lines = append(lines, "  Associations:")
		for _, s := range rt.Associations {
			lines = append(lines, "    "+s)
		}
		lines = append(lines, "")
	}
	if len(rt.Routes) > 0 {
		lines = append(lines, "  Routes:")
		lines = append(lines, fmt.Sprintf("  %-22s %-30s %s", "Destination", "Target", "State"))
		lines = append(lines, "  "+strings.Repeat("─", 60))
		for _, r := range rt.Routes {
			lines = append(lines, fmt.Sprintf("  %-22s %-30s %s", r.Destination, r.Target, r.State))
		}
	}
	lines = append(lines, tagLines(rt.Tags)...)
	return lines
}

func igwDetail(igw IGWInfo) []string {
	lines := []string{
		dl("ID", igw.ID),
		dl("Name", orDash(igw.Name)),
		dl("State", igw.State),
		dl("VPC ID", igw.VPCID),
	}
	lines = append(lines, tagLines(igw.Tags)...)
	return lines
}

func natgwDetail(ngw NatGWInfo) []string {
	lines := []string{
		dl("ID", ngw.ID),
		dl("Name", orDash(ngw.Name)),
		dl("Type", ngw.Type),
		dl("State", ngw.State),
		dl("Subnet ID", ngw.SubnetID),
		dl("VPC ID", ngw.VPCID),
		dl("Public IP", orDash(ngw.PublicIP)),
		dl("Private IP", orDash(ngw.PrivateIP)),
	}
	lines = append(lines, tagLines(ngw.Tags)...)
	return lines
}

func endpointDetail(ep EndpointInfo) []string {
	lines := []string{
		dl("ID", ep.ID),
		dl("Service", ep.ServiceName),
		dl("Type", ep.Type),
		dl("State", ep.State),
		dl("VPC ID", ep.VPCID),
	}
	lines = append(lines, tagLines(ep.Tags)...)
	return lines
}

func naclDetail(nacl NACLInfo) []string {
	lines := []string{
		dl("ID", nacl.ID),
		dl("Name", orDash(nacl.Name)),
		dl("VPC ID", nacl.VPCID),
		dl("Default", boolStr(nacl.IsDefault)),
		dl("Associated subnets", fmt.Sprintf("%d", len(nacl.Associations))),
		"",
	}
	if len(nacl.Rules) > 0 {
		inbound := filterNACLRules(nacl.Rules, "Inbound")
		outbound := filterNACLRules(nacl.Rules, "Outbound")
		sort.Slice(inbound, func(i, j int) bool { return inbound[i].RuleNumber < inbound[j].RuleNumber })
		sort.Slice(outbound, func(i, j int) bool { return outbound[i].RuleNumber < outbound[j].RuleNumber })

		lines = append(lines, "  Inbound Rules:")
		lines = append(lines, fmt.Sprintf("  %-8s %-8s %-10s %-20s %s", "Rule#", "Proto", "Ports", "CIDR", "Action"))
		lines = append(lines, "  "+strings.Repeat("─", 55))
		for _, r := range inbound {
			lines = append(lines, fmt.Sprintf("  %-8d %-8s %-10s %-20s %s", r.RuleNumber, r.Protocol, r.PortRange, r.CIDR, r.Action))
		}
		lines = append(lines, "")
		lines = append(lines, "  Outbound Rules:")
		lines = append(lines, fmt.Sprintf("  %-8s %-8s %-10s %-20s %s", "Rule#", "Proto", "Ports", "CIDR", "Action"))
		lines = append(lines, "  "+strings.Repeat("─", 55))
		for _, r := range outbound {
			lines = append(lines, fmt.Sprintf("  %-8d %-8s %-10s %-20s %s", r.RuleNumber, r.Protocol, r.PortRange, r.CIDR, r.Action))
		}
	}
	lines = append(lines, tagLines(nacl.Tags)...)
	return lines
}

func filterNACLRules(rules []NACLRule, dir string) []NACLRule {
	var out []NACLRule
	for _, r := range rules {
		if r.Direction == dir {
			out = append(out, r)
		}
	}
	return out
}

func peeringDetail(pc PeeringInfo) []string {
	lines := []string{
		dl("ID", pc.ID),
		dl("Status", pc.Status),
		"",
		dl("Requester VPC", pc.RequesterVPCID),
		dl("Requester Region", orDash(pc.RequesterRegion)),
		dl("Requester CIDR", orDash(pc.RequesterCIDR)),
		"",
		dl("Accepter VPC", pc.AccepterVPCID),
		dl("Accepter Region", orDash(pc.AccepterRegion)),
		dl("Accepter CIDR", orDash(pc.AccepterCIDR)),
	}
	lines = append(lines, tagLines(pc.Tags)...)
	return lines
}

func flowLogDetail(fl FlowLogInfo) []string {
	lines := []string{
		dl("ID", fl.ID),
		dl("Resource ID", fl.ResourceID),
		dl("Traffic Type", fl.TrafficType),
		dl("Status", fl.Status),
		dl("Log Destination", fl.LogDestination),
	}
	if fl.LogFormat != "" {
		lines = append(lines, dl("Log Format", fl.LogFormat))
	}
	lines = append(lines, tagLines(fl.Tags)...)
	return lines
}

func ec2Detail(inst EC2InstanceInfo) []string {
	lines := []string{
		dl("Instance ID", inst.ID),
		dl("Name", orDash(inst.Name)),
		dl("State", inst.State),
		dl("Instance Type", inst.Type),
		dl("Platform", inst.Platform),
		dl("Private IP", orDash(inst.PrivateIP)),
		dl("Public IP", orDash(inst.PublicIP)),
		dl("Availability Zone", inst.AZ),
		dl("Subnet ID", inst.SubnetID),
		dl("VPC ID", inst.VPCID),
		dl("Launch Time", orDash(inst.LaunchTime)),
	}
	lines = append(lines, tagLines(inst.Tags)...)
	return lines
}

func lambdaDetail(fn LambdaFunctionInfo) []string {
	lines := []string{
		dl("Function Name", fn.Name),
		dl("Runtime", fn.Runtime),
		dl("State", fn.State),
		dl("Handler", fn.Handler),
		dl("Memory", fmt.Sprintf("%d MB", fn.MemoryMB)),
		dl("Timeout", fmt.Sprintf("%d seconds", fn.TimeoutSec)),
		dl("Last Modified", orDash(fn.LastModified)),
		dl("VPC ID", fn.VPCID),
	}
	if len(fn.SubnetIDs) > 0 {
		lines = append(lines, dl("Subnets", strings.Join(fn.SubnetIDs, ", ")))
	}
	if len(fn.SGIDs) > 0 {
		lines = append(lines, dl("Security Groups", strings.Join(fn.SGIDs, ", ")))
	}
	return lines
}

func rdsDetail(db RDSInstanceInfo) []string {
	return []string{
		dl("DB Identifier", db.ID),
		dl("Engine", db.Engine),
		dl("Instance Class", db.Class),
		dl("Status", db.Status),
		dl("Availability Zone", db.AZ),
		dl("Multi-AZ", boolStr(db.MultiAZ)),
		dl("Storage (GB)", fmt.Sprintf("%d", db.Storage)),
		dl("Endpoint", orDash(db.Endpoint)),
		dl("VPC ID", db.VPCID),
	}
}

func lbDetail(lb LoadBalancerInfo) []string {
	return []string{
		dl("Name", lb.Name),
		dl("Type", lb.Type),
		dl("Scheme", lb.Scheme),
		dl("State", lb.State),
		dl("VPC ID", lb.VPCID),
		dl("DNS Name", lb.DNSName),
		dl("Created", orDash(lb.CreatedAt)),
		dl("ARN", lb.ARN),
	}
}

func tagLines(tags map[string]string) []string {
	if len(tags) == 0 {
		return nil
	}
	lines := []string{"", "  Tags:"}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("    %-20s %s", k, tags[k]))
	}
	return lines
}

// ---------------------------------------------------------------------------
// Shared utilities
// ---------------------------------------------------------------------------

func seqRows(rows []table.Row) []table.Row {
	out := make([]table.Row, len(rows))
	for i, r := range rows {
		nr := make(table.Row, len(r))
		copy(nr, r)
		nr[0] = fmt.Sprintf("%d", i+1)
		out[i] = nr
	}
	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func boolStr(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}
