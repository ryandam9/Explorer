package summary

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// catalogService is one entry in the curated list of common AWS services the
// summary checks for coverage. Key matches the Service value resources carry —
// the collector name for typed services, the ARN namespace for tag-discovered
// ones — so presence can be detected by a simple lookup.
type catalogService struct {
	Key   string
	Label string
}

// commonServices is a hand-picked catalog of widely used AWS services. It is
// deliberately not exhaustive — AWS has hundreds of services and no API returns
// "everything you own" — but it is broad enough that an empty entry is a useful
// signal: either the service genuinely has no resources, or (for the
// tag-discovered ones) it has resources that are untagged and therefore hidden.
//
// Keys for typed services equal the collector name; keys for tag-discovered
// services equal the ARN service namespace. Whether each is actually typed is
// decided at runtime from the engine's registered collectors (see Coverage),
// not from this list — so a service that later gains a collector needs no
// change here.
var commonServices = []catalogService{
	{"ec2", "EC2"},
	{"s3", "S3"},
	{"rds", "RDS"},
	{"iam", "IAM"},
	{"dynamodb", "DynamoDB"},
	{"lambda", "Lambda"},
	{"emr", "EMR"},
	{"ecs", "ECS"},
	{"eks", "EKS"},
	{"elbv2", "ELBv2"},
	{"secretsmanager", "Secrets Manager"},
	{"sqs", "SQS"},
	{"sns", "SNS"},
	{"cloudwatch", "CloudWatch"},
	{"cloudfront", "CloudFront"},
	{"route53", "Route 53"},
	{"apigateway", "API Gateway"},
	{"stepfunctions", "Step Functions"},
	{"eventbridge", "EventBridge"},
	{"elasticache", "ElastiCache"},
	{"efs", "EFS"},
	{"kinesis", "Kinesis"},
	{"redshift", "Redshift"},
	{"kms", "KMS"},
	{"ecr", "ECR"},
	{"acm", "ACM"},
	{"cloudformation", "CloudFormation"},
	{"glue", "Glue"},
	{"athena", "Athena"},
}

// ServiceCoverage describes one common service: whether it has a typed
// collector (complete, tag-independent coverage) and whether any of its
// resources made it into the inventory.
type ServiceCoverage struct {
	Key   string
	Label string
	Typed bool // backed by a typed collector; otherwise tag-discovered only
	Shown bool // at least one resource of this service is in the inventory
}

// catalogWith returns the effective catalog: the built-in commonServices with
// the user-configured extras (summary.commonServices) merged on top, then the
// hidden keys (summary.hideServices) removed. An extra whose key matches a
// built-in one overrides that label; a new key is appended; a blank label falls
// back to the key so a misconfigured entry is still usable. Unknown hide keys
// are ignored.
func catalogWith(extra map[string]string, hide []string) []catalogService {
	if len(extra) == 0 && len(hide) == 0 {
		return commonServices
	}

	out := make([]catalogService, len(commonServices))
	copy(out, commonServices)

	idx := make(map[string]int, len(out))
	for i, c := range out {
		idx[c.Key] = i
	}

	var added []string
	for key, label := range extra {
		if strings.TrimSpace(label) == "" {
			label = key
		}
		if i, ok := idx[key]; ok {
			out[i].Label = label
		} else {
			added = append(added, key)
		}
	}
	// Append new keys in a stable order so output doesn't depend on map ranging.
	sort.Strings(added)
	for _, key := range added {
		label := extra[key]
		if strings.TrimSpace(label) == "" {
			label = key
		}
		out = append(out, catalogService{Key: key, Label: label})
	}

	if len(hide) == 0 {
		return out
	}
	hidden := make(map[string]bool, len(hide))
	for _, k := range hide {
		hidden[k] = true
	}
	kept := out[:0]
	for _, c := range out {
		if !hidden[c.Key] {
			kept = append(kept, c)
		}
	}
	return kept
}

// Coverage compares the curated catalog against the collected inventory.
// typedServices is the set of services that have a typed collector (pass the
// engine's registered collector names); every other catalog service is reached
// only through the tag-based discovery sweep. extra and hide are the
// user-configured additions and removals (summary.commonServices /
// summary.hideServices); pass nil for none.
func Coverage(resources []model.Resource, typedServices []string, extra map[string]string, hide []string) []ServiceCoverage {
	typed := make(map[string]bool, len(typedServices))
	for _, s := range typedServices {
		typed[s] = true
	}
	present := make(map[string]bool)
	for _, r := range resources {
		if r.Service != "" {
			present[r.Service] = true
		}
	}

	catalog := catalogWith(extra, hide)
	out := make([]ServiceCoverage, 0, len(catalog))
	for _, c := range catalog {
		out = append(out, ServiceCoverage{
			Key:   c.Key,
			Label: c.Label,
			Typed: typed[c.Key],
			Shown: present[c.Key],
		})
	}
	return out
}

// NotShown returns the catalog services with no resources in the inventory,
// sorted alphabetically by label for a stable, readable list.
func NotShown(cov []ServiceCoverage) []ServiceCoverage {
	var out []ServiceCoverage
	for _, c := range cov {
		if !c.Shown {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Label < out[j].Label
	})
	return out
}

// CoverageNote renders a plain-language advisory for the summary table,
// listing common services that produced nothing — split by how they're found:
// directly-queried services (absent ⇒ none exist) versus tag-discovered ones
// (absent could just mean untagged). tagSweep reports whether the all-services
// tag search ran (it is skipped by --typed-only). Returns "" when every catalog
// service is present and there is nothing to advise.
//
// The wording deliberately avoids internal terms like "typed collector" — the
// reader is a user trying to understand why something is missing, not how the
// tool is built.
func CoverageNote(cov []ServiceCoverage, tagSweep bool) string {
	missing := NotShown(cov)
	if len(missing) == 0 {
		return ""
	}

	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Bold(true)
	muted := ui.MutedStyle()

	// Split missing services by how they're discovered: a service queried
	// directly that shows nothing genuinely has none, whereas a tag-discovered
	// service might just be untagged. Applying the "may have no tags" caveat to
	// directly-queried services misinforms (#377).
	var direct, tagged []string
	for _, c := range missing {
		if c.Typed {
			direct = append(direct, c.Label)
		} else {
			tagged = append(tagged, c.Label)
		}
	}

	var b strings.Builder
	if len(direct) > 0 {
		b.WriteString(muted.Render("Checked directly, none found: "+strings.Join(direct, ", ")) + "\n")
	}
	if len(tagged) > 0 {
		if tagSweep {
			b.WriteString(warn.Render("⚠ Found only by tags, so any with no tags won't appear: "+strings.Join(tagged, ", ")) + "\n")
		} else {
			b.WriteString(warn.Render("⚠ Not searched (tag lookup skipped): "+strings.Join(tagged, ", ")) + "\n")
			b.WriteString(muted.Render("Run without --typed-only to search for these by tag.") + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
