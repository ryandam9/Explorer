package xref

import (
	"sort"
	"strconv"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// This file generalizes the where-used engine (xref.go) into a bidirectional,
// multi-hop "related resources" query (#337): given any resource, list both
// what it references (forward / "uses") and what references it (reverse /
// "used by"), optionally walking several hops. It builds on the same Edge model
// — collection still emits "From references Target via R" — but indexes and
// walks it in both directions.
//
// Honesty (CLAUDE.md §8) carries over: a result only reflects the relationship
// types collection actually extracts. The reverse direction reuses the scoped
// CheckedTypes list for the recognized kinds; both directions are rendered with
// an explicit "only collected relationships are shown" caveat so an empty side
// never reads as "this resource is isolated".

// Link is one related resource plus how far it sits from the queried resource.
// It embeds Reference so it renders and sorts like a where-used row.
type Link struct {
	Reference
	Depth int    `json:"depth"`          // 1 = directly related, 2 = one hop further, …
	Path  string `json:"path,omitempty"` // chain of relationship labels from the query
}

// RelatedResult is the answer to a bidirectional related-resources query.
type RelatedResult struct {
	Target       Target   `json:"target"`
	Depth        int      `json:"depth"` // max hops walked
	Uses         []Link   `json:"uses"`  // forward: resources the target references
	UsedBy       []Link   `json:"used_by"`
	CheckedTypes []string `json:"checked_types"` // reverse-direction scope (recognized kinds)
	// AllPaths is a render hint (not data): when set, the table shows the full
	// relationship path so multiple paths to one resource are distinguishable.
	AllPaths bool `json:"-"`
	// Partial/Errors carry the best-effort collection status into structured
	// output (§6a): a script reading JSON must be able to tell "no relationships"
	// from "some collectors failed", which stderr alone doesn't convey once the
	// exit code is 0. The caller fills these from Collect's error return.
	Partial bool                 `json:"partial"`
	Errors  []model.ExploreError `json:"errors,omitempty"`
}

// WithCollectionStatus records the best-effort collection errors on the result
// so structured renderers can flag a possibly-incomplete answer.
func (r RelatedResult) WithCollectionStatus(errs []model.ExploreError) RelatedResult {
	r.Partial = len(errs) > 0
	r.Errors = errs
	return r
}

// BuildForwardIndex maps a resource identifier to the edges originating from it,
// so a query can resolve "what does this resource reference". Both the full
// From.ID and its short form are indexed so an ID or ARN query resolves either
// way (mirrors BuildIndex for the reverse direction).
func BuildForwardIndex(edges []Edge) map[string][]Edge {
	idx := make(map[string][]Edge)
	add := func(key string, e Edge) {
		if key == "" {
			return
		}
		idx[key] = append(idx[key], e)
	}
	for _, e := range edges {
		add(e.From.ID, e)
		if short := shortForm(e.From.ID); short != e.From.ID {
			add(short, e)
		}
	}
	return idx
}

// Related answers the bidirectional query for input up to maxDepth hops. fwdIdx
// and revIdx come from BuildForwardIndex and BuildIndex over the same edges.
// When allPaths is set, a resource reachable by several distinct relationship
// paths is emitted once per path (otherwise only the shortest path is kept) —
// the levers behind --show-paths (#388).
func Related(input string, fwdIdx map[string][]Edge, revIdx map[string][]Reference, maxDepth int, allPaths bool) RelatedResult {
	if maxDepth < 1 {
		maxDepth = 1
	}
	target := Classify(input)
	ids := queryIdentifiers(input)
	return RelatedResult{
		Target:       target,
		Depth:        maxDepth,
		Uses:         walkForward(ids, fwdIdx, maxDepth, allPaths),
		UsedBy:       walkReverse(ids, revIdx, maxDepth, allPaths),
		CheckedTypes: CheckedTypes(target.Kind),
		AllPaths:     allPaths,
	}
}

// AmbiguousCandidates returns the distinct fully-qualified identifiers (ARNs)
// in the edge set that share the query's short form, when the query is a bare
// name/short form rather than a full ARN. Short-form matching is convenient but
// can merge unrelated resources — e.g. "app" matching role/team-a/app and
// role/team-b/app, or a name reused across regions (#386). When this returns
// more than one, the caller should warn and suggest passing a full ARN.
// Returns nil when the query is already a full ARN or matches at most one.
func AmbiguousCandidates(input string, edges []Edge) []string {
	in := strings.TrimSpace(input)
	if in == "" || strings.HasPrefix(in, "arn:") {
		return nil
	}
	short := shortForm(in)
	set := map[string]bool{}
	consider := func(id string) {
		// Only count fully-qualified identifiers (ARNs) whose short form equals
		// the query's — those are the genuinely distinct resources a bare name
		// could conflate.
		if strings.HasPrefix(id, "arn:") && shortForm(id) == short {
			set[id] = true
		}
	}
	for _, e := range edges {
		consider(e.From.ID)
		consider(e.Target)
	}
	if len(set) <= 1 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// queryIdentifiers returns the strings a stored edge might match for input —
// the input itself and its short form — so any resource (not just the four
// where-used kinds) is queryable.
func queryIdentifiers(input string) []string {
	in := strings.TrimSpace(input)
	return dedupe(in, shortForm(in))
}

// walkForward performs a breadth-first walk over the forward index: hop 1 is
// what the queried resource references, hop 2 is what those reference, and so
// on. Rows are deduplicated by resource+relationship and cycles are guarded by
// a visited set over both identifier forms.
func walkForward(starts []string, fwdIdx map[string][]Edge, maxDepth int, allPaths bool) []Link {
	visited := newVisited(starts)
	rowSeen := make(map[string]bool)
	var out []Link

	frontier := newFrontier(starts)
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []frontierNode
		for _, node := range frontier {
			for _, e := range lookupEdges(fwdIdx, node.id) {
				ref := targetReference(e)
				path := joinPath(node.path, e.From.Via)
				if rk := dedupeKey(ref, path, allPaths); !rowSeen[rk] {
					rowSeen[rk] = true
					out = append(out, Link{Reference: ref, Depth: depth, Path: path})
				}
				if visited.expand(e.Target) {
					next = append(next, frontierNode{id: e.Target, path: path})
				}
			}
		}
		frontier = next
	}
	SortLinks(out)
	return out
}

// walkReverse mirrors walkForward over the reverse index: hop 1 is what
// references the queried resource, hop 2 is what references those, and so on.
func walkReverse(starts []string, revIdx map[string][]Reference, maxDepth int, allPaths bool) []Link {
	visited := newVisited(starts)
	rowSeen := make(map[string]bool)
	var out []Link

	frontier := newFrontier(starts)
	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var next []frontierNode
		for _, node := range frontier {
			for _, r := range lookupRefs(revIdx, node.id) {
				path := joinPath(node.path, r.Via)
				if rk := dedupeKey(r, path, allPaths); !rowSeen[rk] {
					rowSeen[rk] = true
					out = append(out, Link{Reference: r, Depth: depth, Path: path})
				}
				if visited.expand(r.ID) {
					next = append(next, frontierNode{id: r.ID, path: path})
				}
			}
		}
		frontier = next
	}
	SortLinks(out)
	return out
}

// dedupeKey is the row-dedup key for a walk. By default it's the resource +
// immediate relationship (shortest path wins); with allPaths the full path is
// folded in so a resource reachable by several distinct paths is kept once per
// path (#388). The cycle guard (visited) is independent, so this never loops.
func dedupeKey(ref Reference, path string, allPaths bool) string {
	if allPaths {
		return rowKey(ref) + "\x00" + path
	}
	return rowKey(ref)
}

// --- walk helpers -------------------------------------------------------------

type frontierNode struct {
	id   string
	path string
}

func newFrontier(starts []string) []frontierNode {
	out := make([]frontierNode, 0, len(starts))
	for _, s := range starts {
		out = append(out, frontierNode{id: s})
	}
	return out
}

// visited tracks identifiers already expanded so a hop is never revisited
// (cycle guard). It records both identifier forms for every id.
type visited struct{ seen map[string]bool }

func newVisited(starts []string) *visited {
	v := &visited{seen: make(map[string]bool)}
	for _, s := range starts {
		v.mark(s)
	}
	return v
}

func (v *visited) mark(id string) {
	for _, k := range dedupe(id, shortForm(id)) {
		v.seen[k] = true
	}
}

// expand reports whether id is new (and, if so, marks it visited so the next
// hop walks it exactly once).
func (v *visited) expand(id string) bool {
	for _, k := range dedupe(id, shortForm(id)) {
		if v.seen[k] {
			return false
		}
	}
	v.mark(id)
	return true
}

func lookupEdges(idx map[string][]Edge, id string) []Edge {
	var out []Edge
	for _, k := range dedupe(id, shortForm(id)) {
		out = append(out, idx[k]...)
	}
	return out
}

func lookupRefs(idx map[string][]Reference, id string) []Reference {
	var out []Reference
	for _, k := range dedupe(id, shortForm(id)) {
		out = append(out, idx[k]...)
	}
	return out
}

// rowKey identifies a related row for deduplication: the resource plus the
// relationship it was reached by (depth is excluded so the shortest path wins).
func rowKey(r Reference) string {
	return r.Service + "|" + r.Type + "|" + r.ID + "|" + r.Via
}

// joinPath appends a relationship label to a path chain.
func joinPath(parent, via string) string {
	if parent == "" {
		return via
	}
	return parent + " ▸ " + via
}

// targetReference turns a forward edge into the referenced resource as a row,
// deriving service/type/region from its ARN and carrying the relationship.
func targetReference(e Edge) Reference {
	r := referenceFromIdentifier(e.Target)
	// EC2/Logs short identifiers (sg-…, subnet-…, eni-…, vol-…, /aws/… log
	// groups) carry no region in the string, but they always live in the same
	// region as the resource referencing them — inherit it so the row isn't
	// blank or ambiguous across regions (#385). Names/DNS/ARNs are left as-is
	// (ARNs already carry a region; names stay honestly region-less).
	if r.Region == "" && (r.Service == "ec2" || r.Service == "logs") {
		r.Region = e.From.Region
	}
	r.Via = e.From.Via
	return r
}

// referenceFromIdentifier builds a best-effort Reference from a bare identifier
// or ARN (the forward index only stores the target string, not a struct).
func referenceFromIdentifier(id string) Reference {
	if strings.HasPrefix(id, "arn:") {
		return Reference{
			Service: arnService(id),
			Type:    arnResourceType(id),
			Region:  arnRegion(id),
			ID:      id,
			Name:    shortForm(id),
		}
	}
	r := Reference{ID: id, Name: id}
	if svc, typ, ok := classifyShortID(id); ok {
		r.Service, r.Type = svc, typ
	}
	return r
}

// ec2IDPrefixes maps EC2-style resource-id prefixes to their resource type, so
// a short target id (sg-…, subnet-…, eni-…) renders with a service/type instead
// of a blank cell (#385). All are regional and in the ec2 service.
var ec2IDPrefixes = map[string]string{
	"sg-":       "security-group",
	"subnet-":   "subnet",
	"vpc-":      "vpc",
	"eni-":      "network-interface",
	"vol-":      "volume",
	"ami-":      "image",
	"snap-":     "snapshot",
	"eipalloc-": "elastic-ip",
	"vpce-":     "vpc-endpoint",
	"igw-":      "internet-gateway",
	"nat-":      "nat-gateway",
	"rtb-":      "route-table",
	"acl-":      "network-acl",
	"pcx-":      "vpc-peering-connection",
	"tgw-":      "transit-gateway",
	"dopt-":     "dhcp-options",
	"i-":        "instance",
}

// classifyShortID resolves a bare (non-ARN) identifier to a service/type. It
// recognizes EC2 resource ids and CloudWatch Logs group names; everything else
// (subnet-group names, DNS names, bucket names) stays unclassified so the row
// is honestly blank rather than mislabelled.
func classifyShortID(id string) (service, typ string, ok bool) {
	for prefix, t := range ec2IDPrefixes {
		if strings.HasPrefix(id, prefix) {
			return "ec2", t, true
		}
	}
	if strings.HasPrefix(id, "/aws/") || strings.HasPrefix(id, "/ecs/") {
		return "logs", "log-group", true
	}
	return "", "", false
}

// SortLinks orders links by depth, then like SortReferences within a depth.
func SortLinks(links []Link) {
	sort.SliceStable(links, func(i, j int) bool {
		a, b := links[i], links[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		switch {
		case a.Service != b.Service:
			return a.Service < b.Service
		case a.Type != b.Type:
			return a.Type < b.Type
		case a.Region != b.Region:
			return a.Region < b.Region
		case a.ID != b.ID:
			return a.ID < b.ID
		default:
			return a.Via < b.Via
		}
	})
}

// --- ARN helpers specific to the forward direction ----------------------------

// isARN reports whether s looks like an AWS ARN.
func isARN(s string) bool { return strings.HasPrefix(s, "arn:") }

// arnRegion returns the region field (index 3) of an ARN, "" if malformed.
func arnRegion(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return parts[3]
}

// arnResourceType returns the resource-type segment of an ARN's resource field
// ("role" from "role/app", "function" from "function:name"), "" when the
// resource has no type prefix (e.g. an SQS queue ARN).
func arnResourceType(arn string) string {
	res := arnResource(arn)
	if i := strings.IndexAny(res, "/:"); i > 0 {
		return res[:i]
	}
	return ""
}

// depthLabel renders a hop count for table output.
func depthLabel(d int) string { return strconv.Itoa(d) }
