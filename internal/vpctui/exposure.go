package vpctui

import (
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Public exposure report
//
// One screen answering "what in this VPC is reachable from the internet?":
// public subnets (routing to an internet gateway), security groups open to
// 0.0.0.0/0 (with the exposed ports), and network interfaces holding a public
// IP. Pure over a vpcSnapshot.
// ---------------------------------------------------------------------------

// exposureReport returns the internet-facing surface of a VPC, grouped for
// display. Empty groups are omitted. The first group correlates the three
// ingredients of real exposure — a public IP, an IGW route, and a security
// group open to the internet — into the interfaces that are actually
// reachable, so one permissive-but-unrouted SG doesn't read as an incident.
func exposureReport(snap vpcSnapshot) []xrefGroup {
	var reachableENIs, publicSubnets, openSGs, publicENIs []string

	for _, e := range snap.NetworkInterfaces {
		if e.PublicIP == "" {
			continue
		}
		rt := effectiveRouteTable(snap, e.SubnetID)
		if !hasDefaultRoute(rt, "0.0.0.0/0", "igw-") {
			continue
		}
		ports := publicInboundPorts(snap, e.SecurityGroups)
		if len(ports) == 0 {
			continue
		}
		label := fmt.Sprintf("%s (%s)", e.ID, e.PublicIP)
		if e.AttachedTo != "" && e.AttachedTo != "-" {
			label += " → " + e.AttachedTo
		}
		reachableENIs = append(reachableENIs, label+" — "+strings.Join(ports, ", "))
	}

	for _, s := range snap.Subnets {
		rt := effectiveRouteTable(snap, s.ID)
		if hasDefaultRoute(rt, "0.0.0.0/0", "igw-") || hasDefaultRoute(rt, "::/0", "igw-") {
			publicSubnets = append(publicSubnets, subnetLabel(s))
		}
	}

	for _, sg := range snap.SecurityGroups {
		var ports []string
		seen := map[string]bool{}
		for _, r := range sg.Rules {
			if !strings.EqualFold(r.Direction, "Inbound") || !isPublicSource(r.Source) {
				continue
			}
			p := describeProtoPorts(r.Protocol, r.PortRange)
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
		if len(ports) > 0 {
			openSGs = append(openSGs, fmt.Sprintf("%s — %s", sgLabel(sg), strings.Join(ports, ", ")))
		}
	}

	for _, e := range snap.NetworkInterfaces {
		if e.PublicIP != "" {
			label := fmt.Sprintf("%s (%s)", e.ID, e.PublicIP)
			if e.AttachedTo != "" && e.AttachedTo != "-" {
				label += " → " + e.AttachedTo
			}
			publicENIs = append(publicENIs, label)
		}
	}

	sort.Strings(reachableENIs)
	sort.Strings(publicSubnets)
	sort.Strings(openSGs)
	sort.Strings(publicENIs)

	groups := []xrefGroup{
		{Label: "⚠ Internet-reachable interfaces (public IP + IGW route + open security group)", Items: reachableENIs},
		{Label: "Public subnets (route to an internet gateway)", Items: publicSubnets},
		{Label: "Security groups open to the internet (inbound from 0.0.0.0/0)", Items: openSGs},
		{Label: "Network interfaces with a public IP", Items: publicENIs},
	}
	out := groups[:0]
	for _, g := range groups {
		if len(g.Items) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// publicInboundPorts returns the de-duplicated, sorted list of port phrases
// the given security groups open to the internet (inbound from 0.0.0.0/0 or
// ::/0).
func publicInboundPorts(snap vpcSnapshot, sgIDs []string) []string {
	var ports []string
	seen := map[string]bool{}
	for _, id := range sgIDs {
		sg := findSG(snap, id)
		if sg == nil {
			continue
		}
		for _, r := range sg.Rules {
			if !strings.EqualFold(r.Direction, "Inbound") || !isPublicSource(r.Source) {
				continue
			}
			p := describeProtoPorts(r.Protocol, r.PortRange)
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}
	sort.Strings(ports)
	return ports
}
