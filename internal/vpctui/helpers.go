package vpctui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/user/aws_explorer/internal/display"
	"github.com/user/aws_explorer/internal/table"
)

// ---------------------------------------------------------------------------
// Resource type → config key
// ---------------------------------------------------------------------------

func rtKey(rt resourceType) string {
	switch rt {
	case rtSubnets:
		return "subnets"
	case rtSecurityGroups:
		return "security_groups"
	case rtRouteTables:
		return "route_tables"
	case rtInternetGateways:
		return "internet_gateways"
	case rtNatGateways:
		return "nat_gateways"
	case rtEndpoints:
		return "endpoints"
	case rtNetworkACLs:
		return "network_acls"
	case rtPeering:
		return "peering"
	case rtFlowLogs:
		return "flow_logs"
	case rtEC2Instances:
		return "ec2_instances"
	case rtLambda:
		return "lambda"
	case rtRDS:
		return "rds"
	case rtLoadBalancers:
		return "load_balancers"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Resource type and sidebar
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

func firstSelectableIdx(items []sidebarItem) int {
	for i, item := range items {
		if !item.isHeader {
			return i
		}
	}
	return 0
}

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
// Fetch → []map[string]string
// ---------------------------------------------------------------------------

func fetchResourceMaps(client *VPCClient, rt resourceType, vpcID string) ([]map[string]string, error) {
	switch rt {
	case rtSubnets:
		items, err := client.ListSubnets(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, s := range items {
			out[i] = subnetToMap(s)
		}
		return out, nil

	case rtSecurityGroups:
		items, err := client.ListSecurityGroups(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, sg := range items {
			out[i] = sgToMap(sg)
		}
		return out, nil

	case rtRouteTables:
		items, err := client.ListRouteTables(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, rt := range items {
			out[i] = routeTableToMap(rt)
		}
		return out, nil

	case rtInternetGateways:
		items, err := client.ListInternetGateways(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, igw := range items {
			out[i] = igwToMap(igw)
		}
		return out, nil

	case rtNatGateways:
		items, err := client.ListNatGateways(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, ngw := range items {
			out[i] = natgwToMap(ngw)
		}
		return out, nil

	case rtEndpoints:
		items, err := client.ListVPCEndpoints(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, ep := range items {
			out[i] = endpointToMap(ep)
		}
		return out, nil

	case rtNetworkACLs:
		items, err := client.ListNetworkACLs(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, nacl := range items {
			out[i] = naclToMap(nacl)
		}
		return out, nil

	case rtPeering:
		items, err := client.ListPeeringConnections(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, pc := range items {
			out[i] = peeringToMap(pc)
		}
		return out, nil

	case rtFlowLogs:
		items, err := client.ListFlowLogs(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, fl := range items {
			out[i] = flowLogToMap(fl)
		}
		return out, nil

	case rtEC2Instances:
		items, err := client.ListEC2Instances(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, inst := range items {
			out[i] = ec2ToMap(inst)
		}
		return out, nil

	case rtLambda:
		items, err := client.ListLambdaFunctions(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, fn := range items {
			out[i] = lambdaToMap(fn)
		}
		return out, nil

	case rtRDS:
		items, err := client.ListRDSInstances(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, db := range items {
			out[i] = rdsToMap(db)
		}
		return out, nil

	case rtLoadBalancers:
		items, err := client.ListLoadBalancers(vpcID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]string, len(items))
		for i, lb := range items {
			out[i] = lbToMap(lb)
		}
		return out, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// toMap converters
// ---------------------------------------------------------------------------

func subnetToMap(s SubnetInfo) map[string]string {
	return map[string]string{
		"subnet_id":      s.ID,
		"name":           orDash(s.Name),
		"cidr":           s.CIDR,
		"az":             s.AZ,
		"available_ips":  fmt.Sprintf("%d", s.AvailableIPs),
		"public":         boolStr(s.IsPublic),
		"state":          s.State,
		"vpc_id":         s.VPCID,
		"default_for_az": boolStr(s.DefaultForAz),
		"map_public_ip":  boolStr(s.MapPublicIPOnLaunch),
		"ipv6_cidrs":     strings.Join(s.Ipv6CIDRs, ", "),
		"tags":           display.EncodeTags(s.Tags),
	}
}

func sgToMap(sg SGInfo) map[string]string {
	return map[string]string{
		"sg_id":       sg.ID,
		"name":        sg.Name,
		"description": sg.Description,
		"vpc_id":      sg.VPCID,
		"inbound":     fmt.Sprintf("%d", sg.InboundCount),
		"outbound":    fmt.Sprintf("%d", sg.OutboundCount),
		"rules":       encodeSGRules(sg.Rules),
		"tags":        display.EncodeTags(sg.Tags),
	}
}

func encodeSGRules(rules []SGRule) string {
	if len(rules) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n  %-10s %-8s %-12s %-22s %s", "Dir", "Proto", "Ports", "Source", "Description"))
	sb.WriteString("\n  " + strings.Repeat("─", 70))
	for _, r := range rules {
		desc := r.Description
		if desc == "" {
			desc = "-"
		}
		sb.WriteString(fmt.Sprintf("\n  %-10s %-8s %-12s %-22s %s", r.Direction, r.Protocol, r.PortRange, r.Source, desc))
	}

	// Plain-English explanation of each rule, with risk flags for sensitive
	// ports exposed to the public internet.
	sb.WriteString("\n\n  In plain English:")
	for _, r := range rules {
		sb.WriteString("\n  • " + explainSGRule(r))
	}
	return sb.String()
}

func routeTableToMap(rt RouteTableInfo) map[string]string {
	assocCount := fmt.Sprintf("%d", len(rt.Associations))
	routeCount := fmt.Sprintf("%d", len(rt.Routes))

	var assocLines strings.Builder
	for _, s := range rt.Associations {
		assocLines.WriteString("\n  " + s)
	}

	var routeLines strings.Builder
	routeLines.WriteString(fmt.Sprintf("\n  %-22s %-30s %s", "Destination", "Target", "State"))
	routeLines.WriteString("\n  " + strings.Repeat("─", 60))
	for _, r := range rt.Routes {
		routeLines.WriteString(fmt.Sprintf("\n  %-22s %-30s %s", r.Destination, r.Target, r.State))
	}

	routeList := routeLines.String()
	if len(rt.Associations) > 0 {
		routeList = assocLines.String() + "\n" + routeList
	}

	return map[string]string{
		"rt_id":      rt.ID,
		"name":       orDash(rt.Name),
		"routes":     routeCount,
		"subnets":    assocCount,
		"main":       boolStr(rt.IsMain),
		"vpc_id":     rt.VPCID,
		"route_list": routeList,
		"tags":       display.EncodeTags(rt.Tags),
	}
}

func igwToMap(igw IGWInfo) map[string]string {
	return map[string]string{
		"igw_id": igw.ID,
		"name":   orDash(igw.Name),
		"state":  igw.State,
		"vpc_id": igw.VPCID,
		"tags":   display.EncodeTags(igw.Tags),
	}
}

func natgwToMap(ngw NatGWInfo) map[string]string {
	return map[string]string{
		"nat_id":     ngw.ID,
		"name":       orDash(ngw.Name),
		"nat_type":   ngw.Type,
		"state":      ngw.State,
		"public_ip":  orDash(ngw.PublicIP),
		"private_ip": orDash(ngw.PrivateIP),
		"subnet_id":  ngw.SubnetID,
		"vpc_id":     ngw.VPCID,
		"tags":       display.EncodeTags(ngw.Tags),
	}
}

func endpointToMap(ep EndpointInfo) map[string]string {
	return map[string]string{
		"endpoint_id": ep.ID,
		"service":     ep.ServiceName,
		"ep_type":     ep.Type,
		"state":       ep.State,
		"vpc_id":      ep.VPCID,
		"tags":        display.EncodeTags(ep.Tags),
	}
}

func naclToMap(nacl NACLInfo) map[string]string {
	inbound := filterNACLRules(nacl.Rules, "Inbound")
	outbound := filterNACLRules(nacl.Rules, "Outbound")
	sort.Slice(inbound, func(i, j int) bool { return inbound[i].RuleNumber < inbound[j].RuleNumber })
	sort.Slice(outbound, func(i, j int) bool { return outbound[i].RuleNumber < outbound[j].RuleNumber })

	var sb strings.Builder
	sb.WriteString("\n  Inbound:")
	sb.WriteString(fmt.Sprintf("\n  %-8s %-8s %-10s %-20s %s", "Rule#", "Proto", "Ports", "CIDR", "Action"))
	sb.WriteString("\n  " + strings.Repeat("─", 55))
	for _, r := range inbound {
		sb.WriteString(fmt.Sprintf("\n  %-8d %-8s %-10s %-20s %s", r.RuleNumber, r.Protocol, r.PortRange, r.CIDR, r.Action))
	}
	sb.WriteString("\n\n  Outbound:")
	sb.WriteString(fmt.Sprintf("\n  %-8s %-8s %-10s %-20s %s", "Rule#", "Proto", "Ports", "CIDR", "Action"))
	sb.WriteString("\n  " + strings.Repeat("─", 55))
	for _, r := range outbound {
		sb.WriteString(fmt.Sprintf("\n  %-8d %-8s %-10s %-20s %s", r.RuleNumber, r.Protocol, r.PortRange, r.CIDR, r.Action))
	}

	sb.WriteString(encodeNACLExplanations(inbound, outbound))

	return map[string]string{
		"nacl_id":      nacl.ID,
		"name":         orDash(nacl.Name),
		"rule_count":   fmt.Sprintf("%d", len(nacl.Rules)),
		"subnet_count": fmt.Sprintf("%d", len(nacl.Associations)),
		"is_default":   boolStr(nacl.IsDefault),
		"vpc_id":       nacl.VPCID,
		"rule_list":    sb.String(),
		"tags":         display.EncodeTags(nacl.Tags),
	}
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

func peeringToMap(pc PeeringInfo) map[string]string {
	return map[string]string{
		"peering_id":       pc.ID,
		"status":           pc.Status,
		"requester_vpc":    pc.RequesterVPCID,
		"requester_region": orDash(pc.RequesterRegion),
		"requester_cidr":   orDash(pc.RequesterCIDR),
		"accepter_vpc":     pc.AccepterVPCID,
		"accepter_region":  orDash(pc.AccepterRegion),
		"accepter_cidr":    orDash(pc.AccepterCIDR),
		"tags":             display.EncodeTags(pc.Tags),
	}
}

func flowLogToMap(fl FlowLogInfo) map[string]string {
	return map[string]string{
		"log_id":      fl.ID,
		"resource_id": fl.ResourceID,
		"traffic":     fl.TrafficType,
		"status":      fl.Status,
		"destination": fl.LogDestination,
		"log_format":  fl.LogFormat,
		"tags":        display.EncodeTags(fl.Tags),
	}
}

func ec2ToMap(inst EC2InstanceInfo) map[string]string {
	return map[string]string{
		"instance_id": inst.ID,
		"name":        orDash(inst.Name),
		"state":       inst.State,
		"type":        inst.Type,
		"private_ip":  orDash(inst.PrivateIP),
		"public_ip":   orDash(inst.PublicIP),
		"az":          inst.AZ,
		"platform":    inst.Platform,
		"subnet_id":   inst.SubnetID,
		"vpc_id":      inst.VPCID,
		"iam_role":    orDash(inst.IamRole),
		"ami_id":      orDash(inst.AMIID),
		"key_pair":    orDash(inst.KeyPair),
		"launch_time": orDash(inst.LaunchTime),
		"tags":        display.EncodeTags(inst.Tags),
	}
}

func lambdaToMap(fn LambdaFunctionInfo) map[string]string {
	return map[string]string{
		"name":            fn.Name,
		"runtime":         fn.Runtime,
		"state":           fn.State,
		"handler":         fn.Handler,
		"memory":          fmt.Sprintf("%d MB", fn.MemoryMB),
		"timeout":         fmt.Sprintf("%ds", fn.TimeoutSec),
		"last_modified":   orDash(fn.LastModified),
		"vpc_id":          fn.VPCID,
		"subnets":         strings.Join(fn.SubnetIDs, ", "),
		"security_groups": strings.Join(fn.SGIDs, ", "),
	}
}

func rdsToMap(db RDSInstanceInfo) map[string]string {
	return map[string]string{
		"db_id":    db.ID,
		"engine":   db.Engine,
		"class":    db.Class,
		"status":   db.Status,
		"az":       db.AZ,
		"multi_az": boolStr(db.MultiAZ),
		"storage":  fmt.Sprintf("%d", db.Storage),
		"endpoint": orDash(db.Endpoint),
		"vpc_id":   db.VPCID,
	}
}

func lbToMap(lb LoadBalancerInfo) map[string]string {
	return map[string]string{
		"name":       lb.Name,
		"lb_type":    lb.Type,
		"scheme":     lb.Scheme,
		"state":      lb.State,
		"dns_name":   lb.DNSName,
		"vpc_id":     lb.VPCID,
		"created_at": orDash(lb.CreatedAt),
		"arn":        lb.ARN,
	}
}

// ---------------------------------------------------------------------------
// Shared table helpers
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
