package summary

import (
	"fmt"
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
// services equal the ARN service namespace (e.g. EFS is "elasticfilesystem").
var commonServices = []catalogService{
	// Typed collectors — full coverage regardless of tags.
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
	// Tag-discovered only — untagged resources here can be missing.
	{"elasticache", "ElastiCache"},
	{"elasticfilesystem", "EFS"},
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
// sorted so tag-discovered services (where the absence may hide untagged
// resources) come first, then alphabetically by label.
func NotShown(cov []ServiceCoverage) []ServiceCoverage {
	var out []ServiceCoverage
	for _, c := range cov {
		if !c.Shown {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Typed != out[j].Typed {
			return !out[i].Typed // tag-discovered first
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// CoverageNote renders a highlighted advisory about how complete the inventory
// is: how many services have full typed coverage, the tag-discovery caveat, and
// the common services that produced nothing. typedCount is the number of typed
// collectors; tagSweep reports whether the all-services Tagging API sweep ran
// (it is skipped by --typed-only). Returns "" when every catalog service is
// present and there is nothing to advise.
func CoverageNote(cov []ServiceCoverage, typedCount int, tagSweep bool) string {
	missing := NotShown(cov)
	if len(missing) == 0 {
		return ""
	}

	heading := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorWarning())).Bold(true)
	muted := ui.MutedStyle()

	var b strings.Builder
	b.WriteString(heading.Render("⚠ Coverage") + "\n")
	if tagSweep {
		b.WriteString(muted.Render(fmt.Sprintf(
			"%d services use typed collectors and show every resource. All other services are\n"+
				"tag-discovered — resources that have never been tagged can be missing below.", typedCount)) + "\n")
	} else {
		b.WriteString(muted.Render(fmt.Sprintf(
			"Typed collectors only (--typed-only): the all-services tag sweep was skipped, so the\n"+
				"%d typed services are shown and everything else is omitted.", typedCount)) + "\n")
	}

	b.WriteString("\n" + muted.Render("Common services with nothing shown:") + "\n")
	for _, c := range missing {
		b.WriteString("  • " + c.Label + " — " + coverageReason(c, tagSweep) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// coverageReason explains why a catalog service produced no rows.
func coverageReason(c ServiceCoverage, tagSweep bool) string {
	switch {
	case c.Typed:
		return "typed: none found"
	case !tagSweep:
		return "tag-discovered: not collected (sweep skipped)"
	default:
		return "tag-discovered: none found, or present but untagged (hidden)"
	}
}
