package vpctui

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// VPC Findings — a "linter" for a VPC
//
// The findings engine inspects a snapshot of a VPC's networking resources and
// produces a ranked list of issues, using the same deterministic-heuristic
// approach as the Security Group / Network ACL rule explanations. Every check
// is a pure function over the snapshot so it is straightforward to unit-test.
// ---------------------------------------------------------------------------

// Severity ranks a finding. Higher values sort first.
type Severity int

const (
	SevInfo Severity = iota
	SevWarning
	SevCritical
)

func (s Severity) String() string {
	switch s {
	case SevCritical:
		return "CRITICAL"
	case SevWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}

// Finding is a single detected issue.
type Finding struct {
	Severity Severity
	Resource string // the offending resource ID (or "-")
	Title    string // short one-line summary
	Detail   string // why this was flagged
	Fix      string // suggested remediation
}

// vpcSnapshot bundles the networking resources analyzed together. Slices may be
// empty when a resource type could not be fetched (e.g. missing permissions).
type vpcSnapshot struct {
	VPCID             string
	Region            string // region the snapshot was taken in (baselines only)
	OwnerID           string // account that owns the VPC (baselines only)
	Subnets           []SubnetInfo
	SecurityGroups    []SGInfo
	RouteTables       []RouteTableInfo
	InternetGateways  []IGWInfo
	NatGateways       []NatGWInfo
	NetworkACLs       []NACLInfo
	Peerings          []PeeringInfo
	Endpoints         []EndpointInfo
	NetworkInterfaces []ENIInfo
}

// Thresholds for capacity checks.
const (
	subnetLowIPCount   = 10   // absolute available IPs considered "low"
	subnetHighUtilFrac = 0.90 // utilization fraction considered "high"
)

// analyzeVPC runs every check and returns the findings sorted by severity
// (critical first) then by resource ID for stable output.
func analyzeVPC(snap vpcSnapshot) []Finding {
	var out []Finding
	checkSecurityGroups(snap, &out)
	checkRouteTables(snap, &out)
	checkSubnets(snap, &out)
	checkNatGateways(snap, &out)
	checkInternetGateways(snap, &out)
	checkNACLs(snap, &out)
	checkPeerings(snap, &out)
	checkEndpoints(snap, &out)
	checkOrphans(snap, &out)
	checkQuotas(snap, &out)

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return out[i].Resource < out[j].Resource
	})
	return out
}

// countBySeverity returns how many findings fall in each severity bucket.
func countBySeverity(fs []Finding) (crit, warn, info int) {
	for _, f := range fs {
		switch f.Severity {
		case SevCritical:
			crit++
		case SevWarning:
			warn++
		default:
			info++
		}
	}
	return crit, warn, info
}

// ---------------------------------------------------------------------------
// Security groups
// ---------------------------------------------------------------------------

func checkSecurityGroups(snap vpcSnapshot, out *[]Finding) {
	known := make(map[string]bool, len(snap.SecurityGroups))
	for _, sg := range snap.SecurityGroups {
		known[sg.ID] = true
	}

	for _, sg := range snap.SecurityGroups {
		for _, r := range sg.Rules {
			// Exposure applies to inbound rules only: nearly every SG has the
			// default "all traffic to 0.0.0.0/0" egress rule, which is not an
			// exposure of the resource itself.
			if strings.EqualFold(r.Direction, "Inbound") {
				if note := exposureRisk(r.Protocol, r.PortRange, r.Source); note != "" {
					*out = append(*out, Finding{
						Severity: riskSeverity(note),
						Resource: sg.ID,
						Title:    "Security group exposes a sensitive port to the internet",
						Detail:   sgLabel(sg) + ": " + explainSGRule(r),
						Fix:      "Restrict the source to specific CIDRs or a security group instead of 0.0.0.0/0.",
					})
				}
			}
			if strings.HasPrefix(r.Source, "sg-") && !known[r.Source] {
				*out = append(*out, Finding{
					Severity: SevInfo,
					Resource: sg.ID,
					Title:    "Rule references a security group not found in this VPC",
					Detail: fmt.Sprintf("%s: %s rule references %s, which is not in this VPC (it may be cross-account or deleted).",
						sgLabel(sg), strings.ToLower(r.Direction), r.Source),
					Fix: "Confirm the referenced security group still exists and is intended.",
				})
			}
		}
	}
}

// riskSeverity maps an exposureRisk note to a severity.
func riskSeverity(note string) Severity {
	switch {
	case strings.Contains(note, "ALL ports"),
		strings.Contains(note, "remote admin"),
		strings.Contains(note, "database"),
		strings.Contains(note, "sensitive ports"):
		return SevCritical
	default:
		return SevWarning
	}
}

func sgLabel(sg SGInfo) string {
	if sg.Name != "" && sg.Name != "-" {
		return sg.ID + " (" + sg.Name + ")"
	}
	return sg.ID
}

// ---------------------------------------------------------------------------
// Route tables
// ---------------------------------------------------------------------------

func checkRouteTables(snap vpcSnapshot, out *[]Finding) {
	for _, rt := range snap.RouteTables {
		for _, r := range rt.Routes {
			if strings.EqualFold(r.State, "blackhole") {
				*out = append(*out, Finding{
					Severity: SevWarning,
					Resource: rt.ID,
					Title:    "Route table has a blackhole route",
					Detail: fmt.Sprintf("%s: route to %s points at %s, whose target no longer exists; traffic is dropped.",
						rtLabelName(rt), orUnknownDest(r.Destination), orDash(r.Target)),
					Fix: "Remove or repoint the stale route to a valid target.",
				})
			}
		}
	}
}

func rtLabelName(rt RouteTableInfo) string {
	if rt.Name != "" && rt.Name != "-" {
		return rt.ID + " (" + rt.Name + ")"
	}
	return rt.ID
}

func orUnknownDest(d string) string {
	if d == "" {
		return "(unknown)"
	}
	return d
}

// ---------------------------------------------------------------------------
// Subnets
// ---------------------------------------------------------------------------

func checkSubnets(snap vpcSnapshot, out *[]Finding) {
	for _, s := range snap.Subnets {
		// Capacity.
		usable := usableIPs(s.CIDR)
		avail := int(s.AvailableIPs)
		lowAbsolute := avail < subnetLowIPCount
		highUtil := usable > 0 && float64(usable-avail)/float64(usable) >= subnetHighUtilFrac
		if lowAbsolute || highUtil {
			detail := fmt.Sprintf("%s has %d available IPs", subnetLabel(s), avail)
			if usable > 0 {
				detail += fmt.Sprintf(" of ~%d usable (%.0f%% in use)", usable, 100*float64(usable-avail)/float64(usable))
			}
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: s.ID,
				Title:    "Subnet is running low on IP addresses",
				Detail:   detail + ".",
				Fix:      "Free unused ENIs/IPs, add a secondary CIDR, or move workloads to a larger subnet.",
			})
		}

		// Routing correctness. Auto-assigned public IPs are IPv4, so the
		// map-public-ip check requires an IPv4 default route specifically — an
		// ::/0 → igw- route does not make those addresses usable.
		rt := effectiveRouteTable(snap, s.ID)
		hasIGW := hasDefaultRoute(rt, "0.0.0.0/0", "igw-")
		hasNAT := hasDefaultRoute(rt, "0.0.0.0/0", "nat-")
		// Egress can also flow through an egress-only IGW (IPv6), a transit
		// gateway or peering (centralized egress VPCs), a NAT instance
		// (eni-/i- target), or a virtual private gateway.
		hasOtherEgress := hasDefaultRoute(rt, "0.0.0.0/0", "eigw-", "tgw-", "pcx-", "vgw-", "eni-", "i-") ||
			hasDefaultRoute(rt, "::/0", "igw-", "eigw-", "tgw-", "pcx-")

		if s.MapPublicIPOnLaunch && !hasIGW {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: s.ID,
				Title:    "Subnet auto-assigns public IPs but has no internet gateway route",
				Detail: subnetLabel(s) + " sets map-public-ip-on-launch, but its route table has no 0.0.0.0/0 → internet gateway route, " +
					"so instances get a public IP they cannot use.",
				Fix: "Add an internet gateway route, or disable auto-assign public IP on the subnet.",
			})
		}
		if rt != nil && !hasIGW && !hasNAT && !hasOtherEgress {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: s.ID,
				Title:    "Subnet has no outbound internet path",
				Detail: subnetLabel(s) + " has no default route to an internet gateway, NAT gateway, or other egress target; " +
					"instances cannot reach the internet (this is expected for isolated subnets).",
				Fix: "Add a NAT gateway route for private egress, or an internet gateway route for public subnets — if internet access is needed.",
			})
		}
	}
}

func subnetLabel(s SubnetInfo) string {
	if s.Name != "" && s.Name != "-" {
		return s.ID + " (" + s.Name + ")"
	}
	return s.ID
}

// usableIPs returns the number of assignable IPs in an IPv4 CIDR, accounting for
// the 5 addresses AWS reserves in every subnet. Returns 0 if it cannot parse.
func usableIPs(cidr string) int {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil || ipnet.IP.To4() == nil {
		return 0
	}
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 0 || hostBits > 31 {
		return 0
	}
	total := 1 << uint(hostBits)
	if total <= 5 {
		return 0
	}
	return total - 5
}

// effectiveRouteTable returns the route table a subnet uses: its explicit
// association, or the VPC's main route table as a fallback.
func effectiveRouteTable(snap vpcSnapshot, subnetID string) *RouteTableInfo {
	var main *RouteTableInfo
	for i := range snap.RouteTables {
		rt := &snap.RouteTables[i]
		if rt.IsMain {
			main = rt
		}
		for _, a := range rt.Associations {
			if a == subnetID {
				return rt
			}
		}
	}
	return main
}

// hasDefaultRoute reports whether rt has an active route for exactly dest
// ("0.0.0.0/0" or "::/0") whose target carries one of the given prefixes.
func hasDefaultRoute(rt *RouteTableInfo, dest string, targetPrefixes ...string) bool {
	if rt == nil {
		return false
	}
	for _, r := range rt.Routes {
		if r.Destination != dest || strings.EqualFold(r.State, "blackhole") {
			continue
		}
		for _, p := range targetPrefixes {
			if strings.HasPrefix(r.Target, p) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// NAT gateways
// ---------------------------------------------------------------------------

func checkNatGateways(snap vpcSnapshot, out *[]Finding) {
	referenced := make(map[string]bool)
	for _, rt := range snap.RouteTables {
		for _, r := range rt.Routes {
			if strings.HasPrefix(r.Target, "nat-") {
				referenced[r.Target] = true
			}
		}
	}

	for _, n := range snap.NatGateways {
		if strings.EqualFold(n.State, "available") && !referenced[n.ID] {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: n.ID,
				Title:    "NAT gateway is not referenced by any route",
				Detail: natLabel(n) + " is available but no route table sends 0.0.0.0/0 to it, " +
					"so it carries no traffic while still incurring hourly charges.",
				Fix: "Point a private subnet's default route at this NAT gateway, or delete it to stop charges.",
			})
		}
		if n.State != "" && !strings.EqualFold(n.State, "available") {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: n.ID,
				Title:    "NAT gateway is not in the available state",
				Detail:   fmt.Sprintf("%s is in state %q.", natLabel(n), n.State),
				Fix:      "Investigate the NAT gateway status if private subnets rely on it for egress.",
			})
		}
	}
}

func natLabel(n NatGWInfo) string {
	if n.Name != "" && n.Name != "-" {
		return n.ID + " (" + n.Name + ")"
	}
	return n.ID
}

// ---------------------------------------------------------------------------
// Internet gateways
// ---------------------------------------------------------------------------

func checkInternetGateways(snap vpcSnapshot, out *[]Finding) {
	for _, igw := range snap.InternetGateways {
		if igw.VPCID == "" || (igw.State != "" && !strings.EqualFold(igw.State, "available") && !strings.EqualFold(igw.State, "attached")) {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: igw.ID,
				Title:    "Internet gateway is not attached to this VPC",
				Detail:   fmt.Sprintf("%s is not attached (state %q); public subnets in this VPC have no internet path.", igw.ID, orDash(igw.State)),
				Fix:      "Attach the internet gateway to the VPC if public connectivity is required.",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Network ACLs
// ---------------------------------------------------------------------------

func checkNACLs(snap vpcSnapshot, out *[]Finding) {
	for _, nacl := range snap.NetworkACLs {
		// The default NACL ships allowing all traffic, but its rules are fully
		// editable — a hardened default NACL bites exactly like a custom one,
		// so it is evaluated the same way (an intact allow-all passes anyway).
		//
		// A subnet that serves inbound connections needs outbound ephemeral ports
		// open for the responses, and vice versa. We only treat targeted
		// service-port allows (not the ephemeral return-range rule itself) as
		// evidence that a direction initiates/serves connections, to avoid
		// flagging the correct "inbound 443 + outbound 1024-65535" pattern.
		inService := naclHasServiceAllow(nacl, "Inbound")
		outService := naclHasServiceAllow(nacl, "Outbound")

		if inService && !naclAllowsEphemeral(nacl, "Outbound") {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: nacl.ID,
				Title:    "Network ACL may block return traffic",
				Detail: naclLabel(nacl) + " allows inbound service traffic but has no outbound allow covering ephemeral ports 1024-65535. " +
					"NACLs are stateless, so replies to inbound connections will be dropped.",
				Fix: "Add an outbound allow rule for TCP/UDP ports 1024-65535 to the appropriate destinations.",
			})
		}
		if outService && !naclAllowsEphemeral(nacl, "Inbound") {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: nacl.ID,
				Title:    "Network ACL may block return traffic",
				Detail: naclLabel(nacl) + " allows outbound service traffic but has no inbound allow covering ephemeral ports 1024-65535. " +
					"NACLs are stateless, so replies to outbound connections will be dropped.",
				Fix: "Add an inbound allow rule for TCP/UDP ports 1024-65535 from the appropriate sources.",
			})
		}
	}
}

// naclHasServiceAllow reports whether the NACL has an allow rule in the given
// direction for a targeted service port — i.e. anything other than the broad
// ephemeral return range (1024-65535), which is a response rule, not a service.
func naclHasServiceAllow(nacl NACLInfo, dir string) bool {
	for _, r := range nacl.Rules {
		if !strings.EqualFold(r.Action, "allow") || !strings.EqualFold(r.Direction, dir) {
			continue
		}
		if isEphemeralReturnRange(r.PortRange) {
			continue
		}
		return true
	}
	return false
}

// isEphemeralReturnRange reports whether a port range is the ephemeral return
// range (a numeric range that spans 1024-65535). "All" is not treated as an
// ephemeral-only rule because it also serves traffic.
func isEphemeralReturnRange(portRange string) bool {
	ports := strings.TrimSpace(portRange)
	if strings.EqualFold(ports, "All") {
		return false
	}
	from, to, ok := parsePortRange(ports)
	return ok && from <= 1024 && to >= 65535
}

// naclAllowsEphemeral reports whether TCP ephemeral return traffic
// (represented by port 32768) can pass the NACL in the given direction. Rules
// are evaluated in ascending rule-number order with first-match-wins, the way
// AWS does: an early matching deny that covers every source (0.0.0.0/0 or
// ::/0) blocks the return path even if a later rule would allow it, while a
// narrower deny may still leave room for later allows, so evaluation continues
// past it.
func naclAllowsEphemeral(nacl NACLInfo, dir string) bool {
	for _, r := range rulesForDir(&nacl, dir) {
		if !protoMatch(r.Protocol, "tcp") || !portMatch(r.PortRange, 32768) {
			continue
		}
		if strings.EqualFold(r.Action, "allow") {
			return true
		}
		if r.CIDR == "0.0.0.0/0" || r.CIDR == "::/0" {
			return false
		}
	}
	return false
}

func naclLabel(n NACLInfo) string {
	if n.Name != "" && n.Name != "-" {
		return n.ID + " (" + n.Name + ")"
	}
	return n.ID
}

// ---------------------------------------------------------------------------
// Peering connections
// ---------------------------------------------------------------------------

func checkPeerings(snap vpcSnapshot, out *[]Finding) {
	for _, p := range snap.Peerings {
		// Compare every requester CIDR against every accepter CIDR — secondary
		// CIDR blocks overlap just as fatally as the primary ones.
		if reqCIDR, accCIDR, overlap := peeringOverlap(p); overlap {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: p.ID,
				Title:    "Peering connection has overlapping CIDRs",
				Detail: fmt.Sprintf("%s: requester %s and accepter %s overlap; cross-VPC routing to the overlap is ambiguous and will not work.",
					p.ID, reqCIDR, accCIDR),
				Fix: "Re-IP one VPC so the peered CIDRs do not overlap.",
			})
		}
		if p.Status != "" && !strings.EqualFold(p.Status, "active") {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: p.ID,
				Title:    "Peering connection is not active",
				Detail:   fmt.Sprintf("%s is in state %q.", p.ID, p.Status),
				Fix:      "Accept or recreate the peering connection if traffic should flow across it.",
			})
		}
	}
}

// peeringOverlap returns the first overlapping requester/accepter CIDR pair.
// All CIDR blocks of each side (primary plus secondaries, when fetched) are
// compared pairwise.
func peeringOverlap(p PeeringInfo) (string, string, bool) {
	for _, a := range peeringSideCIDRs(p.RequesterCIDR, p.RequesterCIDRs) {
		for _, b := range peeringSideCIDRs(p.AccepterCIDR, p.AccepterCIDRs) {
			if cidrsOverlap(a, b) {
				return a, b, true
			}
		}
	}
	return "", "", false
}

// peeringSideCIDRs merges a side's primary CIDR with its CIDR set (baselines
// saved before secondary CIDRs were captured have only the primary).
func peeringSideCIDRs(primary string, set []string) []string {
	out := make([]string, 0, len(set)+1)
	seen := map[string]bool{}
	for _, c := range append([]string{primary}, set...) {
		c = strings.TrimSpace(c)
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// cidrsOverlap reports whether two IPv4/IPv6 CIDRs share any addresses.
func cidrsOverlap(a, b string) bool {
	_, na, errA := net.ParseCIDR(strings.TrimSpace(a))
	_, nb, errB := net.ParseCIDR(strings.TrimSpace(b))
	if errA != nil || errB != nil {
		return false
	}
	return na.Contains(nb.IP) || nb.Contains(na.IP)
}

// ---------------------------------------------------------------------------
// Capacity / service quotas
//
// AWS enforces per-resource limits whose breach produces cryptic API errors.
// These are the well-known default values (all adjustable via Service Quotas);
// we flag usage that is approaching or at the default ceiling.
// ---------------------------------------------------------------------------

const (
	quotaRulesPerSGDir   = 60  // inbound or outbound rules per security group
	quotaRoutesPerTable  = 50  // non-propagated routes per route table
	quotaSGsPerENI       = 5   // security groups per network interface (max 16)
	quotaSubnetsPerVPC   = 200 // subnets per VPC
	quotaRulesPerNACLDir = 20  // inbound or outbound rules per network ACL (max 40)
	quotaWarnFraction    = 0.8 // warn once usage reaches this fraction of the limit
)

func checkQuotas(snap vpcSnapshot, out *[]Finding) {
	warnAt := func(limit int) int { return int(float64(limit) * quotaWarnFraction) }

	// Rules per security group, per direction.
	for _, sg := range snap.SecurityGroups {
		in, eg := 0, 0
		for _, r := range sg.Rules {
			if strings.EqualFold(r.Direction, "Inbound") {
				in++
			} else {
				eg++
			}
		}
		for _, d := range []struct {
			name  string
			count int
		}{{"inbound", in}, {"outbound", eg}} {
			if d.count < warnAt(quotaRulesPerSGDir) {
				continue
			}
			sev := SevWarning
			if d.count >= quotaRulesPerSGDir {
				sev = SevCritical
			}
			*out = append(*out, Finding{
				Severity: sev,
				Resource: sg.ID,
				Title:    "Security group is approaching its rule limit",
				Detail: fmt.Sprintf("%s has %d %s rules (default limit %d per direction).",
					sgLabel(sg), d.count, d.name, quotaRulesPerSGDir),
				Fix: "Consolidate rules (e.g. use prefix lists or referenced security groups) or request a quota increase.",
			})
		}
	}

	// Routes per route table.
	for _, rt := range snap.RouteTables {
		n := len(rt.Routes)
		if n < warnAt(quotaRoutesPerTable) {
			continue
		}
		sev := SevWarning
		if n >= quotaRoutesPerTable {
			sev = SevCritical
		}
		*out = append(*out, Finding{
			Severity: sev,
			Resource: rt.ID,
			Title:    "Route table is approaching its route limit",
			Detail:   fmt.Sprintf("%s has %d routes (default limit %d).", rtLabelName(rt), n, quotaRoutesPerTable),
			Fix:      "Consolidate CIDRs, use prefix lists, or request a route-table quota increase.",
		})
	}

	// Rules per network ACL, per direction. The immutable default "*" rule
	// (number 32767) does not count against the quota.
	for _, nacl := range snap.NetworkACLs {
		in, eg := 0, 0
		for _, r := range nacl.Rules {
			if r.RuleNumber >= 32767 {
				continue
			}
			if strings.EqualFold(r.Direction, "Inbound") {
				in++
			} else {
				eg++
			}
		}
		for _, d := range []struct {
			name  string
			count int
		}{{"inbound", in}, {"outbound", eg}} {
			if d.count < warnAt(quotaRulesPerNACLDir) {
				continue
			}
			sev := SevWarning
			if d.count >= quotaRulesPerNACLDir {
				sev = SevCritical
			}
			*out = append(*out, Finding{
				Severity: sev,
				Resource: nacl.ID,
				Title:    "Network ACL is approaching its rule limit",
				Detail: fmt.Sprintf("%s has %d %s rules (default limit %d per direction, raisable to 40).",
					naclLabel(nacl), d.count, d.name, quotaRulesPerNACLDir),
				Fix: "Consolidate CIDRs into broader rules or request a network-ACL rule quota increase.",
			})
		}
	}

	// Security groups per network interface.
	for _, e := range snap.NetworkInterfaces {
		if len(e.SecurityGroups) >= quotaSGsPerENI {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: e.ID,
				Title:    "Network interface is at the default security-group limit",
				Detail: fmt.Sprintf("%s uses %d security groups (default limit %d, raisable to 16).",
					e.ID, len(e.SecurityGroups), quotaSGsPerENI),
				Fix: "Request a quota increase if more groups are needed, or consolidate rules into fewer groups.",
			})
		}
	}

	// Subnets per VPC.
	if n := len(snap.Subnets); n >= warnAt(quotaSubnetsPerVPC) {
		sev := SevWarning
		if n >= quotaSubnetsPerVPC {
			sev = SevCritical
		}
		res := snap.VPCID
		if res == "" {
			res = "-"
		}
		*out = append(*out, Finding{
			Severity: sev,
			Resource: res,
			Title:    "VPC is approaching its subnet limit",
			Detail:   fmt.Sprintf("this VPC has %d subnets (default limit %d).", n, quotaSubnetsPerVPC),
			Fix:      "Request a subnets-per-VPC quota increase, or split workloads across VPCs.",
		})
	}
}

// ---------------------------------------------------------------------------
// VPC endpoints
//
// "My private instance can't reach S3 / Secrets Manager" usually means a
// gateway endpoint with no route-table association, or an interface endpoint
// whose security group blocks HTTPS or whose private DNS is off.
// ---------------------------------------------------------------------------

func checkEndpoints(snap vpcSnapshot, out *[]Finding) {
	for _, ep := range snap.Endpoints {
		switch {
		case strings.EqualFold(ep.Type, "Gateway"):
			if len(ep.RouteTableIDs) == 0 {
				*out = append(*out, Finding{
					Severity: SevWarning,
					Resource: ep.ID,
					Title:    "Gateway endpoint is not associated with any route table",
					Detail: fmt.Sprintf("%s (%s) has no route-table association, so no subnet can route to the service through it.",
						ep.ID, ep.ServiceName),
					Fix: "Associate the gateway endpoint with the route tables of the subnets that need it.",
				})
			}
		case strings.EqualFold(ep.Type, "Interface"):
			if !endpointSGsAllowHTTPS(snap, ep) {
				*out = append(*out, Finding{
					Severity: SevWarning,
					Resource: ep.ID,
					Title:    "Interface endpoint's security groups do not allow inbound HTTPS",
					Detail: fmt.Sprintf("%s (%s) has no security-group rule allowing inbound TCP 443, so clients in the VPC cannot reach it.",
						ep.ID, ep.ServiceName),
					Fix: "Add an inbound TCP 443 rule from the VPC CIDR (or client security groups) to the endpoint's security group.",
				})
			}
			if !ep.PrivateDNSEnabled {
				*out = append(*out, Finding{
					Severity: SevInfo,
					Resource: ep.ID,
					Title:    "Interface endpoint has private DNS disabled",
					Detail: fmt.Sprintf("%s (%s) has private DNS disabled; clients must use the endpoint-specific DNS name rather than the standard service name.",
						ep.ID, ep.ServiceName),
					Fix: "Enable private DNS on the endpoint (requires the VPC's DNS support and hostnames to be on).",
				})
			}
		}

		if ep.State != "" && !strings.EqualFold(ep.State, "available") {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: ep.ID,
				Title:    "VPC endpoint is not in the available state",
				Detail:   fmt.Sprintf("%s (%s) is in state %q.", ep.ID, ep.ServiceName, ep.State),
				Fix:      "Investigate the endpoint status if workloads depend on it.",
			})
		}
	}
}

// endpointSGsAllowHTTPS reports whether any of an interface endpoint's security
// groups has an inbound rule permitting TCP 443.
func endpointSGsAllowHTTPS(snap vpcSnapshot, ep EndpointInfo) bool {
	for _, sgID := range ep.SecurityGroups {
		sg := findSG(snap, sgID)
		if sg == nil {
			continue
		}
		for _, r := range sg.Rules {
			if strings.EqualFold(r.Direction, "Inbound") &&
				protoMatch(r.Protocol, "tcp") && portMatch(r.PortRange, 443) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Orphan / unused resources
//
// Idle resources clutter a VPC and (for NAT gateways, covered above) cost
// money. These checks need the ENI inventory to know what is actually in use.
// ---------------------------------------------------------------------------

func checkOrphans(snap vpcSnapshot, out *[]Finding) {
	// Skip orphan checks entirely when we have no ENI data, to avoid
	// false "unused" findings from a partial snapshot.
	if len(snap.NetworkInterfaces) == 0 {
		return
	}

	sgUsed := map[string]bool{}
	subnetUsed := map[string]bool{}
	for _, e := range snap.NetworkInterfaces {
		subnetUsed[e.SubnetID] = true
		for _, g := range e.SecurityGroups {
			sgUsed[g] = true
		}
	}
	// A security group is also "in use" if another group references it.
	for _, sg := range snap.SecurityGroups {
		for _, r := range sg.Rules {
			if strings.HasPrefix(r.Source, "sg-") {
				sgUsed[r.Source] = true
			}
		}
	}

	for _, sg := range snap.SecurityGroups {
		if strings.EqualFold(sg.Name, "default") {
			continue // the default SG is expected to linger unused
		}
		if !sgUsed[sg.ID] {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: sg.ID,
				Title:    "Security group appears unused",
				Detail:   sgLabel(sg) + " is not attached to any network interface and is not referenced by other security groups.",
				Fix:      "Delete the security group if it is no longer needed.",
			})
		}
	}

	for _, s := range snap.Subnets {
		if !subnetUsed[s.ID] {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: s.ID,
				Title:    "Subnet has no network interfaces",
				Detail:   subnetLabel(s) + " contains no network interfaces (it may be unused).",
				Fix:      "Reclaim the subnet's CIDR or remove it if it is no longer needed.",
			})
		}
	}
}
