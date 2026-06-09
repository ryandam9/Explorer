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
			if note := exposureRisk(r.Protocol, r.PortRange, r.Source); note != "" {
				*out = append(*out, Finding{
					Severity: riskSeverity(note),
					Resource: sg.ID,
					Title:    "Security group exposes a sensitive port to the internet",
					Detail:   sgLabel(sg) + ": " + explainSGRule(r),
					Fix:      "Restrict the source to specific CIDRs or a security group instead of 0.0.0.0/0.",
				})
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
		strings.Contains(note, "database"):
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

		// Routing correctness.
		rt := effectiveRouteTable(snap, s.ID)
		hasIGW := routeTableHasInternet(rt, "igw-")
		hasNAT := routeTableHasInternet(rt, "nat-")

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
		if rt != nil && !hasIGW && !hasNAT {
			*out = append(*out, Finding{
				Severity: SevInfo,
				Resource: s.ID,
				Title:    "Subnet has no outbound internet path",
				Detail: subnetLabel(s) + " has no 0.0.0.0/0 route to an internet gateway or NAT gateway; " +
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

// routeTableHasInternet reports whether rt has a default (0.0.0.0/0 or ::/0)
// route whose target has the given prefix (e.g. "igw-" or "nat-").
func routeTableHasInternet(rt *RouteTableInfo, targetPrefix string) bool {
	if rt == nil {
		return false
	}
	for _, r := range rt.Routes {
		if (r.Destination == "0.0.0.0/0" || r.Destination == "::/0") &&
			strings.HasPrefix(r.Target, targetPrefix) &&
			!strings.EqualFold(r.State, "blackhole") {
			return true
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
		if nacl.IsDefault {
			continue // default NACL allows all; the stateless trap only bites custom ones
		}
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

// naclAllowsEphemeral reports whether the NACL has an allow rule in the given
// direction that covers the ephemeral port range (all ports, or a range that
// spans 1024-65535).
func naclAllowsEphemeral(nacl NACLInfo, dir string) bool {
	for _, r := range nacl.Rules {
		if !strings.EqualFold(r.Action, "allow") || !strings.EqualFold(r.Direction, dir) {
			continue
		}
		ports := strings.TrimSpace(r.PortRange)
		if strings.EqualFold(ports, "All") {
			return true
		}
		if from, to, ok := parsePortRange(ports); ok && from <= 1024 && to >= 65535 {
			return true
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
		if cidrsOverlap(p.RequesterCIDR, p.AccepterCIDR) {
			*out = append(*out, Finding{
				Severity: SevWarning,
				Resource: p.ID,
				Title:    "Peering connection has overlapping CIDRs",
				Detail: fmt.Sprintf("%s: requester %s and accepter %s overlap; cross-VPC routing to the overlap is ambiguous and will not work.",
					p.ID, p.RequesterCIDR, p.AccepterCIDR),
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
	quotaRulesPerSGDir  = 60  // inbound or outbound rules per security group
	quotaRoutesPerTable = 50  // non-propagated routes per route table
	quotaSGsPerENI      = 5   // security groups per network interface (max 16)
	quotaSubnetsPerVPC  = 200 // subnets per VPC
	quotaWarnFraction   = 0.8 // warn once usage reaches this fraction of the limit
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
