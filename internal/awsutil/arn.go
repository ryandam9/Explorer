package awsutil

import (
	"fmt"
	"strings"
)

// EC2ARN builds an ARN for an EC2-namespace resource (instance, vpc, subnet,
// volume, …). AWS does not return ARNs for most EC2 resources, so they must be
// constructed from the region, account ID, resource type and resource ID.
// Returns "" when account or id is empty, since a partial ARN is misleading.
func EC2ARN(region, account, resourceType, id string) string {
	if account == "" || id == "" {
		return ""
	}
	return fmt.Sprintf("arn:aws:ec2:%s:%s:%s/%s", region, account, resourceType, id)
}

// S3BucketARN builds the ARN for an S3 bucket. Bucket ARNs are global and carry
// neither region nor account ID.
func S3BucketARN(name string) string {
	if name == "" {
		return ""
	}
	return "arn:aws:s3:::" + name
}

// Route53ZoneARN builds the ARN for a hosted zone. zoneID may arrive as either
// "Z123" or "/hostedzone/Z123"; the prefix is stripped. Route53 ARNs are global.
func Route53ZoneARN(zoneID string) string {
	id := strings.TrimPrefix(zoneID, "/hostedzone/")
	if id == "" {
		return ""
	}
	return "arn:aws:route53:::hostedzone/" + id
}

// SQSARNFromURL derives a queue ARN from its URL. An SQS URL has the shape
// https://sqs.<region>.amazonaws.com/<account>/<name>, which already contains
// the account ID and queue name needed for the ARN. Returns "" if the URL does
// not parse into the expected segments.
func SQSARNFromURL(queueURL string) string {
	const marker = ".amazonaws.com/"
	i := strings.Index(queueURL, marker)
	if i < 0 {
		return ""
	}
	rest := queueURL[i+len(marker):] // "<account>/<name>"
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return ""
	}
	account := rest[:slash]
	name := rest[slash+1:]
	region := sqsRegionFromURL(queueURL)
	if account == "" || name == "" || region == "" {
		return ""
	}
	return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, account, name)
}

// ARN holds the parsed components of an AWS ARN.
type ARN struct {
	Partition    string
	Service      string
	Region       string
	AccountID    string
	ResourceType string
	ResourceID   string
}

// ParseARN splits an ARN into its components. ARNs have the shape
//
//	arn:partition:service:region:account-id:resource
//
// where the trailing resource segment is one of "id", "type/id" or "type:id"
// (and the id itself may contain further "/" separators, e.g. an ELB ARN).
// Returns false if the string is not a well-formed ARN.
func ParseARN(arn string) (ARN, bool) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return ARN{}, false
	}
	out := ARN{
		Partition: parts[1],
		Service:   parts[2],
		Region:    parts[3],
		AccountID: parts[4],
	}
	// Some services (e.g. API Gateway) use a leading-slash resource of the form
	// "/restapis/<id>"; trim it so the first segment is treated as the type.
	resource := strings.TrimPrefix(parts[5], "/")
	switch {
	case strings.Contains(resource, "/"):
		i := strings.IndexByte(resource, '/')
		out.ResourceType = resource[:i]
		out.ResourceID = resource[i+1:]
	case strings.Contains(resource, ":"):
		i := strings.IndexByte(resource, ':')
		out.ResourceType = resource[:i]
		out.ResourceID = resource[i+1:]
	default:
		out.ResourceID = resource
	}
	return out, true
}

// CanonicalService maps an AWS ARN service namespace to the canonical name the
// typed collectors use. The Resource Groups Tagging API reports a resource by
// its ARN namespace (e.g. "elasticmapreduce"), while the typed collectors emit
// the shorter name the AWS CLI uses ("emr"); without this mapping the same
// service shows up twice in the summary — once per code path. The canonical
// form is the collector/CLI name (also the console-link key), so the ARN
// namespace is normalized to it. Unknown namespaces are returned lower-cased
// unchanged, so this only ever merges known aliases.
func CanonicalService(s string) string {
	switch strings.ToLower(s) {
	case "elasticmapreduce":
		return "emr"
	case "elasticloadbalancing", "elasticloadbalancingv2":
		return "elbv2"
	case "elasticfilesystem":
		return "efs"
	case "events":
		return "eventbridge"
	case "states":
		return "stepfunctions"
	default:
		return strings.ToLower(s)
	}
}

// ARNName returns a human-friendly name for a parsed ARN: the last path segment
// of its resource ID (e.g. "i-0abc" from "instance/i-0abc", or the final
// component of a multi-segment ELB resource ID).
func (a ARN) ARNName() string {
	id := a.ResourceID
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		id = id[i+1:]
	}
	return id
}

// sqsRegionFromURL extracts the region from an SQS URL of the form
// https://sqs.<region>.amazonaws.com/...
func sqsRegionFromURL(queueURL string) string {
	const prefix = "sqs."
	i := strings.Index(queueURL, prefix)
	if i < 0 {
		return ""
	}
	rest := queueURL[i+len(prefix):]
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return ""
	}
	return rest[:dot]
}
