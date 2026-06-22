// Package xref implements the generalized where-used / blast-radius query
// (AXE-009): "can I delete this?" answered for the resources people actually
// ask about — IAM roles, KMS keys, ACM certificates and security groups.
//
// The summary TUI's `x` does a blind substring scan of the collected
// inventory; this package does typed reference resolution: collection (see
// collect.go) emits Edges — "resource X references identifier V as
// relationship R" — by reading the linking fields the inventory does not keep
// (a Lambda's execution role, a volume's KMS key, a listener's certificate),
// and the pure functions here index and query them.
//
// Crucially, a "not referenced" answer is scoped: WhereUsed always reports the
// list of reference types that were actually checked for the target's kind, so
// absence of evidence is never presented as proof of absence.
package xref

import (
	"sort"
	"strings"
)

// Kind is the classified type of the queried resource.
type Kind string

const (
	KindIAMRole       Kind = "iam-role"
	KindKMSKey        Kind = "kms-key"
	KindACMCert       Kind = "acm-certificate"
	KindSecurityGroup Kind = "security-group"
	KindUnknown       Kind = "unknown"
)

// Target is the resource the user asked about, resolved to the identifier
// strings a reference might carry.
type Target struct {
	Kind        Kind     `json:"kind"`
	Input       string   `json:"input"`         // the original argument
	ARN         string   `json:"arn,omitempty"` // full ARN, when known
	ID          string   `json:"id,omitempty"`  // short identifier (role name, key id, sg-…)
	Identifiers []string `json:"-"`             // every string a reference might match
}

// Reference is one resource that refers to the target.
type Reference struct {
	Service string `json:"service"`
	Type    string `json:"type"`
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Region  string `json:"region"`
	Via     string `json:"via"` // the relationship, e.g. "execution role"
}

// Edge is a typed reference emitted by collection: the From resource points at
// Target (an ARN or short ID) via the relationship recorded in From.Via.
type Edge struct {
	From   Reference
	Target string
}

// Result is the answer to a where-used query.
type Result struct {
	Target       Target      `json:"target"`
	References   []Reference `json:"references"`
	CheckedTypes []string    `json:"checked_types"`
}

// Classify determines the kind of a resource ID or ARN and extracts the
// identifier strings a reference might match. It is deliberately conservative:
// an input it cannot place is KindUnknown rather than a wrong guess.
func Classify(input string) Target {
	in := strings.TrimSpace(input)
	t := Target{Input: in}
	switch {
	case strings.HasPrefix(in, "arn:"):
		t.ARN = in
		classifyARN(&t, in)
	case strings.HasPrefix(in, "sg-"):
		t.Kind = KindSecurityGroup
		t.ID = in
	default:
		// A bare name is ambiguous; treat it as an IAM role name (the most
		// common bare-name where-used query) so callers still get a scoped
		// answer, but keep Kind explicit.
		if in != "" {
			t.Kind = KindIAMRole
			t.ID = in
		} else {
			t.Kind = KindUnknown
		}
	}
	t.Identifiers = dedupe(t.ARN, t.ID)
	return t
}

func classifyARN(t *Target, arn string) {
	service := arnService(arn)
	resource := arnResource(arn)
	switch service {
	case "iam":
		if name, ok := strings.CutPrefix(resource, "role/"); ok {
			t.Kind = KindIAMRole
			t.ID = lastSegment(name) // role name (drop any path)
			return
		}
	case "kms":
		if id, ok := strings.CutPrefix(resource, "key/"); ok {
			t.Kind = KindKMSKey
			t.ID = id
			return
		}
	case "acm":
		if id, ok := strings.CutPrefix(resource, "certificate/"); ok {
			t.Kind = KindACMCert
			t.ID = id
			return
		}
	case "ec2":
		if id, ok := strings.CutPrefix(resource, "security-group/"); ok {
			t.Kind = KindSecurityGroup
			t.ID = id
			return
		}
	}
	t.Kind = KindUnknown
}

// BuildIndex maps every referenced identifier to the resources that reference
// it. Both the exact edge target and its short form (an ARN's trailing
// segment) are indexed, so a query by ID resolves references stored by ARN and
// vice versa.
func BuildIndex(edges []Edge) map[string][]Reference {
	idx := make(map[string][]Reference)
	add := func(key string, ref Reference) {
		if key == "" {
			return
		}
		idx[key] = append(idx[key], ref)
	}
	for _, e := range edges {
		add(e.Target, e.From)
		if short := shortForm(e.Target); short != e.Target {
			add(short, e.From)
		}
	}
	return idx
}

// WhereUsed answers the query: which resources reference the target, and which
// reference types were checked for its kind.
func WhereUsed(target Target, index map[string][]Reference) Result {
	seen := make(map[string]bool)
	var refs []Reference
	for _, id := range target.Identifiers {
		for _, key := range dedupe(id, shortForm(id)) {
			for _, r := range index[key] {
				k := r.Service + "|" + r.Type + "|" + r.ID + "|" + r.Via
				if seen[k] {
					continue
				}
				seen[k] = true
				refs = append(refs, r)
			}
		}
	}
	SortReferences(refs)
	return Result{
		Target:       target,
		References:   refs,
		CheckedTypes: CheckedTypes(target.Kind),
	}
}

// CheckedTypes returns the human-readable reference categories that collection
// scans for a given kind — the basis of the scoped "not referenced" answer.
func CheckedTypes(kind Kind) []string {
	switch kind {
	case KindIAMRole:
		return []string{
			"Lambda execution roles",
			"EC2 instance profiles",
			"ECS task and execution roles",
			"EKS cluster and node-group roles",
			"IAM role trust policies",
			"S3 bucket replication roles",
			"Step Functions execution roles",
		}
	case KindKMSKey:
		return []string{
			"EBS volume encryption",
			"RDS instance encryption",
			"Secrets Manager secrets",
			"SQS queue encryption",
			"Lambda environment encryption",
			"S3 bucket default encryption",
			"EFS file system encryption",
			"SNS topic encryption",
			"Kinesis stream encryption",
		}
	case KindACMCert:
		return []string{
			"ELBv2 (ALB/NLB) listeners",
			"CloudFront distribution viewer certificates",
		}
	case KindSecurityGroup:
		return []string{
			"Elastic network interface attachments",
			"EFS mount target security groups",
			"Lambda VPC security groups",
			"EKS cluster security groups",
			"Load balancer security groups",
			"API Gateway VPC link security groups",
			"VPC endpoint security groups",
		}
	default:
		return nil
	}
}

// SortReferences orders references deterministically by service, type, region
// then ID.
func SortReferences(refs []Reference) {
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
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

// --- ARN / identifier helpers -------------------------------------------------

// arnService returns the service field (index 2) of an ARN, "" if malformed.
func arnService(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return parts[2]
}

// arnResource returns the resource portion (field 5) of an ARN.
func arnResource(arn string) string {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 {
		return ""
	}
	return parts[5]
}

// shortForm reduces an ARN (or path-bearing identifier) to its trailing
// segment, used so ID and ARN queries resolve to the same references.
func shortForm(s string) string {
	if !strings.HasPrefix(s, "arn:") {
		return s
	}
	res := arnResource(s)
	// resource may be "type/name", "type:name" or just "name"; take the tail.
	res = lastSegment(res)
	if i := strings.LastIndexByte(res, ':'); i >= 0 && i+1 < len(res) {
		res = res[i+1:]
	}
	return res
}

// lastSegment returns the text after the final '/', or the input unchanged.
func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

// dedupe returns the non-empty inputs with duplicates removed, order preserved.
func dedupe(vals ...string) []string {
	seen := make(map[string]bool, len(vals))
	var out []string
	for _, v := range vals {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
