package vpctui

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// Cross-reference ("where used")
//
// Given a resource ID, crossReference walks the VPC snapshot and reports both
// what references the resource and what it references, turning the flat
// resource tables into a navigable relationship graph. Every function is pure
// over a vpcSnapshot and unit-testable.
// ---------------------------------------------------------------------------

// xrefGroup is one labelled set of related resources.
type xrefGroup struct {
	Label string
	Items []string
}

// crossReference returns the relationship groups for a resource, dispatched by
// its ID prefix. Empty groups are omitted. The bool reports whether the
// resource type is cross-referenced at all, so the UI can tell "no relations"
// apart from "not supported".
func crossReference(snap vpcSnapshot, id string) ([]xrefGroup, bool) {
	var groups []xrefGroup
	switch {
	case strings.HasPrefix(id, "sg-"):
		groups = xrefSecurityGroup(snap, id)
	case strings.HasPrefix(id, "subnet-"):
		groups = xrefSubnet(snap, id)
	case strings.HasPrefix(id, "rtb-"):
		groups = xrefRouteTable(snap, id)
	case strings.HasPrefix(id, "eni-"):
		groups = xrefENI(snap, id)
	case strings.HasPrefix(id, "nat-"):
		groups = xrefNatGateway(snap, id)
	case strings.HasPrefix(id, "igw-"):
		groups = xrefInternetGateway(snap, id)
	case strings.HasPrefix(id, "acl-"):
		groups = xrefNACL(snap, id)
	case strings.HasPrefix(id, "vpce-"):
		groups = xrefEndpoint(snap, id)
	case strings.HasPrefix(id, "pcx-"):
		groups = xrefPeering(snap, id)
	default:
		return nil, false
	}
	// Drop empty groups for a clean display.
	out := groups[:0]
	for _, g := range groups {
		if len(g.Items) > 0 {
			out = append(out, g)
		}
	}
	return out, true
}

// xrefSupported reports whether the "where used" overlay can cross-reference
// the resources of a sidebar category, so the key hint is only advertised
// where it works.
func xrefSupported(rt resourceType) bool {
	switch rt {
	case rtSecurityGroups, rtSubnets, rtRouteTables, rtNetworkInterfaces,
		rtNatGateways, rtInternetGateways, rtNetworkACLs, rtEndpoints, rtPeering:
		return true
	default:
		return false
	}
}

// xrefSupportedTypes is shown when x is pressed on an unsupported resource.
const xrefSupportedTypes = "security groups, subnets, route tables, network interfaces, " +
	"NAT gateways, internet gateways, network ACLs, VPC endpoints, and peering connections"

func xrefSecurityGroup(snap vpcSnapshot, id string) []xrefGroup {
	var enis, refs []string
	for _, e := range snap.NetworkInterfaces {
		if contains(e.SecurityGroups, id) {
			label := e.ID
			if e.AttachedTo != "" && e.AttachedTo != "-" {
				label += " → " + e.AttachedTo
			}
			enis = append(enis, label)
		}
	}
	for _, sg := range snap.SecurityGroups {
		if sg.ID == id {
			continue
		}
		for _, r := range sg.Rules {
			if r.Source == id {
				refs = append(refs, fmt.Sprintf("%s (%s rule)", sgLabel(sg), strings.ToLower(r.Direction)))
				break
			}
		}
	}
	return []xrefGroup{
		{Label: "Attached to network interfaces", Items: enis},
		{Label: "Referenced by other security groups", Items: refs},
	}
}

func xrefSubnet(snap vpcSnapshot, id string) []xrefGroup {
	var rtItems, naclItems, eniItems, natItems []string

	explicitRT := false
	var mainRT string
	for _, rt := range snap.RouteTables {
		if rt.IsMain {
			mainRT = rtLabelName(rt)
		}
		if contains(rt.Associations, id) {
			rtItems = append(rtItems, rtLabelName(rt))
			explicitRT = true
		}
	}
	if !explicitRT && mainRT != "" {
		rtItems = append(rtItems, mainRT+" (main, implicit)")
	}

	explicitNACL := false
	var defNACL string
	for _, n := range snap.NetworkACLs {
		if n.IsDefault {
			defNACL = naclLabel(n)
		}
		if contains(n.Associations, id) {
			naclItems = append(naclItems, naclLabel(n))
			explicitNACL = true
		}
	}
	if !explicitNACL && defNACL != "" {
		naclItems = append(naclItems, defNACL+" (default)")
	}

	for _, e := range snap.NetworkInterfaces {
		if e.SubnetID == id {
			eniItems = append(eniItems, e.ID)
		}
	}
	for _, n := range snap.NatGateways {
		if n.SubnetID == id {
			natItems = append(natItems, natLabel(n))
		}
	}
	return []xrefGroup{
		{Label: "Route table", Items: rtItems},
		{Label: "Network ACL", Items: naclItems},
		{Label: "Network interfaces in subnet", Items: eniItems},
		{Label: "NAT gateways in subnet", Items: natItems},
	}
}

func xrefRouteTable(snap vpcSnapshot, id string) []xrefGroup {
	var subnets, targets []string
	seen := map[string]bool{}
	for _, rt := range snap.RouteTables {
		if rt.ID != id {
			continue
		}
		subnets = append(subnets, rt.Associations...)
		for _, r := range rt.Routes {
			if r.Target == "" || r.Target == "local" || seen[r.Target] {
				continue
			}
			seen[r.Target] = true
			targets = append(targets, fmt.Sprintf("%s (%s)", r.Target, routeTargetKind(r.Target)))
		}
	}
	return []xrefGroup{
		{Label: "Associated subnets", Items: subnets},
		{Label: "Routes to", Items: targets},
	}
}

func xrefENI(snap vpcSnapshot, id string) []xrefGroup {
	for _, e := range snap.NetworkInterfaces {
		if e.ID != id {
			continue
		}
		var attached []string
		if e.AttachedTo != "" && e.AttachedTo != "-" {
			attached = append(attached, e.AttachedTo)
		}
		var subnet []string
		if e.SubnetID != "" {
			subnet = append(subnet, e.SubnetID)
		}
		return []xrefGroup{
			{Label: "Attached to", Items: attached},
			{Label: "Subnet", Items: subnet},
			{Label: "Security groups", Items: e.SecurityGroups},
		}
	}
	return nil
}

func xrefNatGateway(snap vpcSnapshot, id string) []xrefGroup {
	var subnet, routedBy []string
	for _, n := range snap.NatGateways {
		if n.ID == id && n.SubnetID != "" {
			subnet = append(subnet, n.SubnetID)
		}
	}
	routedBy = routeTablesTargeting(snap, id)
	return []xrefGroup{
		{Label: "In subnet", Items: subnet},
		{Label: "Routed to by route tables", Items: routedBy},
	}
}

func xrefInternetGateway(snap vpcSnapshot, id string) []xrefGroup {
	return []xrefGroup{
		{Label: "Routed to by route tables", Items: routeTablesTargeting(snap, id)},
	}
}

func xrefNACL(snap vpcSnapshot, id string) []xrefGroup {
	var subnets []string
	for _, n := range snap.NetworkACLs {
		if n.ID == id {
			subnets = append(subnets, n.Associations...)
		}
	}
	return []xrefGroup{
		{Label: "Associated subnets", Items: subnets},
	}
}

func xrefEndpoint(snap vpcSnapshot, id string) []xrefGroup {
	for _, ep := range snap.Endpoints {
		if ep.ID != id {
			continue
		}
		var svc []string
		if ep.ServiceName != "" {
			label := ep.ServiceName
			if ep.Type != "" {
				label += " (" + strings.ToLower(ep.Type) + " endpoint)"
			}
			svc = append(svc, label)
		}
		return []xrefGroup{
			{Label: "Service", Items: svc},
			{Label: "Associated route tables", Items: ep.RouteTableIDs},
			{Label: "Subnets", Items: ep.SubnetIDs},
			{Label: "Security groups", Items: ep.SecurityGroups},
			{Label: "Routed to by route tables", Items: routeTablesTargeting(snap, id)},
		}
	}
	return nil
}

func xrefPeering(snap vpcSnapshot, id string) []xrefGroup {
	var sides []string
	for _, p := range snap.Peerings {
		if p.ID != id {
			continue
		}
		if p.RequesterVPCID != "" {
			sides = append(sides, "Requester: "+p.RequesterVPCID+peeringCIDRSuffix(p.RequesterCIDR, p.RequesterCIDRs))
		}
		if p.AccepterVPCID != "" {
			sides = append(sides, "Accepter: "+p.AccepterVPCID+peeringCIDRSuffix(p.AccepterCIDR, p.AccepterCIDRs))
		}
	}
	return []xrefGroup{
		{Label: "Peered VPCs", Items: sides},
		{Label: "Routed to by route tables", Items: routeTablesTargeting(snap, id)},
	}
}

func peeringCIDRSuffix(primary string, set []string) string {
	cidrs := peeringSideCIDRs(primary, set)
	if len(cidrs) == 0 {
		return ""
	}
	return " (" + strings.Join(cidrs, ", ") + ")"
}

// routeTablesTargeting returns route tables with a route whose target is id.
func routeTablesTargeting(snap vpcSnapshot, id string) []string {
	var out []string
	for _, rt := range snap.RouteTables {
		for _, r := range rt.Routes {
			if r.Target == id {
				out = append(out, fmt.Sprintf("%s (route %s)", rtLabelName(rt), r.Destination))
				break
			}
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
