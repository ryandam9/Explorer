package vpctui

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Connectivity path tracer
//
// Given a source ENI and a destination IP/port, tracePath walks the forward
// path the way AWS evaluates it — source security-group egress, source NACL
// egress, route-table lookup, then (for in-VPC destinations) the destination
// NACL ingress and security-group ingress — and reports the first hop that
// blocks the connection. Because NACLs are stateless it also checks that the
// ephemeral return traffic is permitted.
//
// Every step is a pure function over a vpcSnapshot so the logic is fully
// unit-testable without any AWS calls.
// ---------------------------------------------------------------------------

// hopStatus is the outcome of a single step in the path.
type hopStatus int

const (
	hopPass hopStatus = iota
	hopFail
	hopNote
)

func (s hopStatus) String() string {
	switch s {
	case hopFail:
		return "BLOCKED"
	case hopNote:
		return "NOTE"
	default:
		return "OK"
	}
}

// traceHop is one evaluated step along the path.
type traceHop struct {
	Status hopStatus
	Name   string
	Detail string
}

// traceRequest describes the connection to evaluate.
type traceRequest struct {
	SourceENIID string
	DestIP      string // destination IPv4 address ("internet" is mapped to a public IP)
	Protocol    string // tcp | udp | all
	Port        int    // -1 = any
}

// traceResult is the outcome of a path trace.
type traceResult struct {
	Reachable bool
	Summary   string
	Hops      []traceHop
}

// tracePath evaluates the forward (and stateless return) path for req.
func tracePath(snap vpcSnapshot, req traceRequest) traceResult {
	res := traceResult{}
	add := func(st hopStatus, name, detail string) {
		res.Hops = append(res.Hops, traceHop{Status: st, Name: name, Detail: detail})
	}
	fail := func(name, detail string) traceResult {
		add(hopFail, name, detail)
		res.Reachable = false
		res.Summary = "❌ Blocked at: " + name
		return res
	}

	src := findENI(snap, req.SourceENIID)
	if src == nil {
		return fail("Source", "source network interface "+req.SourceENIID+" not found")
	}
	destIP := net.ParseIP(normalizeDestIP(req.DestIP))
	if destIP == nil {
		return fail("Destination", "could not parse destination IP "+req.DestIP)
	}
	dst := findENIByIP(snap, destIP.String())

	add(hopNote, "Source",
		fmt.Sprintf("%s (%s) in subnet %s", src.ID, orDash(src.PrivateIP), orDash(src.SubnetID)))

	// Managed prefix lists cannot be expanded from a snapshot, so any rule or
	// route that uses one is skipped by the matchers below. Flag that up front
	// so a "Blocked" verdict is not read as definitive.
	if hasPrefixListElements(snap, src, dst) {
		add(hopNote, "Prefix lists",
			"some security-group rules or routes reference managed prefix lists (pl-…), which this trace cannot expand; the verdict may be incomplete")
	}

	// 1. Source security-group egress (stateful).
	if ok, why := sgEgressAllows(snap, src, destIP, dst, req); ok {
		add(hopPass, "Security group egress", why)
	} else {
		return fail("Security group egress", why)
	}

	// 2. Source NACL egress (stateless).
	srcNACL := naclForSubnet(snap, src.SubnetID)
	if ok, why := naclAllows(srcNACL, "Outbound", req.Protocol, req.Port, destIP); ok {
		add(hopPass, "Source NACL egress", why)
	} else {
		return fail("Source NACL egress", why)
	}

	// 3. Route-table lookup for the destination.
	rt := effectiveRouteTable(snap, src.SubnetID)
	route, ok := longestPrefixRoute(rt, destIP)
	if !ok {
		return fail("Route table", "no route matches "+destIP.String())
	}
	if strings.EqualFold(route.State, "blackhole") {
		return fail("Route table", fmt.Sprintf("route to %s is a blackhole (target %s no longer exists)", route.Destination, orDash(route.Target)))
	}
	add(hopPass, "Route table", fmt.Sprintf("%s → %s (%s)", route.Destination, orDash(route.Target), routeTargetKind(route.Target)))

	internal := strings.EqualFold(route.Target, "local")

	switch {
	case internal:
		// In-VPC destination: evaluate the destination side.
		if dst == nil {
			add(hopNote, "Destination", destIP.String()+" is inside the VPC CIDR but no ENI currently uses that address")
			res.Reachable = true
			res.Summary = "⚠ Path is open, but no resource is using the destination address"
			return res
		}
		// 4. Destination NACL ingress.
		dstNACL := naclForSubnet(snap, dst.SubnetID)
		if ok, why := naclAllows(dstNACL, "Inbound", req.Protocol, req.Port, ipOf(src.PrivateIP)); ok {
			add(hopPass, "Destination NACL ingress", why)
		} else {
			return fail("Destination NACL ingress", why)
		}
		// 5. Destination security-group ingress.
		if ok, why := sgIngressAllows(snap, dst, ipOf(src.PrivateIP), src, req); ok {
			add(hopPass, "Destination security group ingress", why)
		} else {
			return fail("Destination security group ingress", why)
		}
		// Stateless return: destination NACL egress + source NACL ingress for ephemeral ports.
		if ok, why := naclAllowsEphemeralReturn(dstNACL, "Outbound", ipOf(src.PrivateIP)); !ok {
			return fail("Destination NACL return (stateless)", why)
		}
		if ok, why := naclAllowsEphemeralReturn(srcNACL, "Inbound", ipOf(dst.PrivateIP)); !ok {
			return fail("Source NACL return (stateless)", why)
		}
		res.Reachable = true
		res.Summary = "✅ Reachable: " + src.ID + " → " + dst.ID
		return res

	case strings.HasPrefix(route.Target, "igw-"):
		// Internet via internet gateway: the instance needs a public IP/EIP.
		if src.PublicIP == "" {
			return fail("Internet gateway", src.ID+" has no public IP or Elastic IP, so it cannot use the internet gateway for direct internet access")
		}
		add(hopPass, "Internet gateway", "egress via "+route.Target+" with public IP "+src.PublicIP)
		res.Reachable = true
		res.Summary = "✅ Reachable: internet via internet gateway"
		return res

	case strings.HasPrefix(route.Target, "nat-"):
		add(hopPass, "NAT gateway", "private egress to the internet via "+route.Target)
		res.Reachable = true
		res.Summary = "✅ Reachable: internet via NAT gateway"
		return res

	default:
		add(hopNote, "Egress target", fmt.Sprintf("routed to %s (%s); reachability beyond this point is not evaluated", route.Target, routeTargetKind(route.Target)))
		res.Reachable = true
		res.Summary = "⚠ Path is open up to " + route.Target
		return res
	}
}

// ---------------------------------------------------------------------------
// Lookups
// ---------------------------------------------------------------------------

func findENI(snap vpcSnapshot, id string) *ENIInfo {
	for i := range snap.NetworkInterfaces {
		if snap.NetworkInterfaces[i].ID == id {
			return &snap.NetworkInterfaces[i]
		}
	}
	return nil
}

func findENIByIP(snap vpcSnapshot, ip string) *ENIInfo {
	for i := range snap.NetworkInterfaces {
		if snap.NetworkInterfaces[i].PrivateIP == ip {
			return &snap.NetworkInterfaces[i]
		}
	}
	return nil
}

// hasPrefixListElements reports whether any rule on the source/destination
// ENI's security groups, or any route on the source subnet's route table,
// references a managed prefix list. Those elements cannot be evaluated from a
// snapshot, so the caller surfaces a caveat.
func hasPrefixListElements(snap vpcSnapshot, src, dst *ENIInfo) bool {
	sgHasPL := func(eni *ENIInfo) bool {
		if eni == nil {
			return false
		}
		for _, sgID := range eni.SecurityGroups {
			sg := findSG(snap, sgID)
			if sg == nil {
				continue
			}
			for _, r := range sg.Rules {
				if strings.HasPrefix(strings.TrimSpace(r.Source), "pl-") {
					return true
				}
			}
		}
		return false
	}
	if sgHasPL(src) || sgHasPL(dst) {
		return true
	}
	if src != nil {
		if rt := effectiveRouteTable(snap, src.SubnetID); rt != nil {
			for _, r := range rt.Routes {
				if strings.HasPrefix(r.Destination, "pl-") {
					return true
				}
			}
		}
	}
	return false
}

// naclForSubnet returns the NACL explicitly associated with a subnet, or the
// default NACL as a fallback.
func naclForSubnet(snap vpcSnapshot, subnetID string) *NACLInfo {
	var def *NACLInfo
	for i := range snap.NetworkACLs {
		n := &snap.NetworkACLs[i]
		if n.IsDefault {
			def = n
		}
		for _, a := range n.Associations {
			if a == subnetID {
				return n
			}
		}
	}
	return def
}

// longestPrefixRoute returns the most-specific active route whose destination
// CIDR contains ip. Prefix-list routes are skipped (their CIDRs are unknown).
func longestPrefixRoute(rt *RouteTableInfo, ip net.IP) (*Route, bool) {
	if rt == nil {
		return nil, false
	}
	var best *Route
	bestOnes := -1
	for i := range rt.Routes {
		r := &rt.Routes[i]
		_, ipnet, err := net.ParseCIDR(r.Destination)
		if err != nil || !ipnet.Contains(ip) {
			continue
		}
		if ones, _ := ipnet.Mask.Size(); ones > bestOnes {
			bestOnes = ones
			best = r
		}
	}
	return best, best != nil
}

func normalizeDestIP(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "internet", "any", "0.0.0.0/0", "0.0.0.0":
		return "1.1.1.1" // a representative public address for routing
	}
	if i := strings.IndexByte(s, '/'); i >= 0 { // tolerate a /32 etc.
		s = s[:i]
	}
	return s
}

func ipOf(s string) net.IP { return net.ParseIP(strings.TrimSpace(s)) }

func routeTargetKind(target string) string {
	switch {
	case target == "local":
		return "local"
	case strings.HasPrefix(target, "igw-"):
		return "internet gateway"
	case strings.HasPrefix(target, "nat-"):
		return "NAT gateway"
	case strings.HasPrefix(target, "pcx-"):
		return "VPC peering"
	case strings.HasPrefix(target, "vpce-"):
		return "VPC endpoint"
	case strings.HasPrefix(target, "tgw-"):
		return "transit gateway"
	case strings.HasPrefix(target, "eigw-"):
		return "egress-only internet gateway"
	case strings.HasPrefix(target, "cagw-"):
		return "carrier gateway"
	case strings.HasPrefix(target, "lgw-"):
		return "local gateway"
	default:
		return "gateway"
	}
}

// ---------------------------------------------------------------------------
// Security-group matching
// ---------------------------------------------------------------------------

func sgEgressAllows(snap vpcSnapshot, src *ENIInfo, destIP net.IP, dst *ENIInfo, req traceRequest) (bool, string) {
	for _, sgID := range src.SecurityGroups {
		sg := findSG(snap, sgID)
		if sg == nil {
			continue
		}
		for _, r := range sg.Rules {
			if !strings.EqualFold(r.Direction, "Outbound") {
				continue
			}
			if protoMatch(r.Protocol, req.Protocol) && portMatch(r.PortRange, req.Port) &&
				sgPeerMatches(r.Source, destIP, dst) {
				return true, fmt.Sprintf("%s allows %s", sgID, describeProtoPorts(r.Protocol, r.PortRange))
			}
		}
	}
	return false, fmt.Sprintf("no egress rule on %s allows %s to %s",
		strings.Join(src.SecurityGroups, "/"), describeReqPorts(req), destIP)
}

func sgIngressAllows(snap vpcSnapshot, dst *ENIInfo, srcIP net.IP, src *ENIInfo, req traceRequest) (bool, string) {
	for _, sgID := range dst.SecurityGroups {
		sg := findSG(snap, sgID)
		if sg == nil {
			continue
		}
		for _, r := range sg.Rules {
			if !strings.EqualFold(r.Direction, "Inbound") {
				continue
			}
			if protoMatch(r.Protocol, req.Protocol) && portMatch(r.PortRange, req.Port) &&
				sgPeerMatches(r.Source, srcIP, src) {
				return true, fmt.Sprintf("%s allows %s", sgID, describeProtoPorts(r.Protocol, r.PortRange))
			}
		}
	}
	return false, fmt.Sprintf("no ingress rule on %s allows %s from %s",
		strings.Join(dst.SecurityGroups, "/"), describeReqPorts(req), srcIP)
}

func findSG(snap vpcSnapshot, id string) *SGInfo {
	for i := range snap.SecurityGroups {
		if snap.SecurityGroups[i].ID == id {
			return &snap.SecurityGroups[i]
		}
	}
	return nil
}

// sgPeerMatches reports whether a rule's peer (a CIDR or a referenced security
// group) covers the given IP / ENI.
func sgPeerMatches(peer string, ip net.IP, eni *ENIInfo) bool {
	peer = strings.TrimSpace(peer)
	switch {
	case peer == "" || peer == "-":
		return false
	case strings.HasPrefix(peer, "sg-"):
		if eni == nil {
			return false
		}
		for _, g := range eni.SecurityGroups {
			if g == peer {
				return true
			}
		}
		return false
	default:
		return cidrContainsIP(peer, ip)
	}
}

// ---------------------------------------------------------------------------
// NACL matching (stateless, ordered, first-match-wins)
// ---------------------------------------------------------------------------

// naclAllows evaluates the NACL's rules for the given direction in ascending
// rule-number order and returns whether the first matching rule allows traffic.
func naclAllows(nacl *NACLInfo, dir, proto string, port int, ip net.IP) (bool, string) {
	if nacl == nil {
		return true, "no NACL associated (default allow)"
	}
	rules := rulesForDir(nacl, dir)
	for _, r := range rules {
		if !protoMatch(r.Protocol, proto) || !portMatch(r.PortRange, port) || !cidrContainsIP(r.CIDR, ip) {
			continue
		}
		if strings.EqualFold(r.Action, "allow") {
			return true, fmt.Sprintf("%s rule %d allows it", nacl.ID, r.RuleNumber)
		}
		return false, fmt.Sprintf("%s rule %d denies it", nacl.ID, r.RuleNumber)
	}
	return false, fmt.Sprintf("%s has no matching %s allow rule (implicit deny)", nacl.ID, strings.ToLower(dir))
}

// naclAllowsEphemeralReturn checks that the NACL permits the ephemeral return
// traffic (ports 1024-65535) in the given direction from/to ip.
func naclAllowsEphemeralReturn(nacl *NACLInfo, dir string, ip net.IP) (bool, string) {
	if nacl == nil {
		return true, ""
	}
	// 32768 is a representative ephemeral port covered by 1024-65535.
	ok, _ := naclAllows(nacl, dir, "tcp", 32768, ip)
	if ok {
		return true, ""
	}
	return false, fmt.Sprintf("%s does not allow ephemeral return ports (1024-65535) %s; NACLs are stateless",
		nacl.ID, directionWord(dir))
}

func directionWord(dir string) string {
	if strings.EqualFold(dir, "Outbound") {
		return "outbound"
	}
	return "inbound"
}

func rulesForDir(nacl *NACLInfo, dir string) []NACLRule {
	var out []NACLRule
	for _, r := range nacl.Rules {
		if strings.EqualFold(r.Direction, dir) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RuleNumber < out[j].RuleNumber })
	return out
}

// ---------------------------------------------------------------------------
// Protocol / port / CIDR matchers
// ---------------------------------------------------------------------------

func protoMatch(ruleProto, reqProto string) bool {
	r := strings.ToLower(strings.TrimSpace(ruleProto))
	q := strings.ToLower(strings.TrimSpace(reqProto))
	if r == "" || r == "all" || r == "-1" {
		return true
	}
	if q == "" || q == "all" {
		return true
	}
	return r == q
}

func portMatch(rulePortRange string, reqPort int) bool {
	ports := strings.TrimSpace(rulePortRange)
	if strings.EqualFold(ports, "All") || ports == "" {
		return true
	}
	if reqPort < 0 {
		return false // a specific rule cannot satisfy an "any port" request
	}
	if !strings.Contains(ports, "-") {
		p, ok := atoiPort(ports)
		return ok && p == reqPort
	}
	from, to, ok := parsePortRange(ports)
	return ok && reqPort >= from && reqPort <= to
}

func cidrContainsIP(cidr string, ip net.IP) bool {
	if ip == nil {
		return false
	}
	cidr = strings.TrimSpace(cidr)
	if !strings.Contains(cidr, "/") {
		// A bare IP: treat as /32 (or /128) host match.
		return cidr != "" && net.ParseIP(cidr).Equal(ip)
	}
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return ipnet.Contains(ip)
}

func describeReqPorts(req traceRequest) string {
	if req.Port < 0 {
		return strings.ToUpper(req.Protocol)
	}
	return fmt.Sprintf("%s %d", strings.ToUpper(req.Protocol), req.Port)
}
