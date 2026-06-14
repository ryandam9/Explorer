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

// Coverage compares the curated catalog against the collected inventory.
// typedServices is the set of services that have a typed collector (pass the
// engine's registered collector names); every other catalog service is reached
// only through the tag-based discovery sweep.
func Coverage(resources []model.Resource, typedServices []string) []ServiceCoverage {
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

	out := make([]ServiceCoverage, 0, len(commonServices))
	for _, c := range commonServices {
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

// CoverageNote renders a plain-language advisory for the summary table: if an
// expected resource is absent, it may simply have no tags, followed by the
// common services that produced nothing. tagSweep reports whether the
// all-services tag search ran (it is skipped by --typed-only). Returns "" when
// every catalog service is present and there is nothing to advise.
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

	var b strings.Builder
	b.WriteString(warn.Render("⚠ If a resource you expected isn't listed, it may have no tags, or there simply aren't any.") + "\n")
	if !tagSweep {
		b.WriteString(muted.Render("Run without --typed-only to also search for resources by their tags.") + "\n")
	}

	names := make([]string, len(missing))
	for i, c := range missing {
		names[i] = c.Label
	}
	b.WriteString(muted.Render("Common services with nothing shown: " + strings.Join(names, ", ")))
	return b.String()
}
