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
