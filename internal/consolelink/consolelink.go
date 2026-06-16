// Package consolelink turns resources into AWS console deep links — pure
// string work that saves a minute of console clicking every time. Unknown
// types fall back to the console's ARN search URL (which resolves almost
// anything), so every resource yields *some* valid link.
package consolelink

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// URL returns a console link for the resource and whether it is a
// type-specific deep link (true) or a generic fallback (false). The deep
// link is built from service/type/region/ID when present; resources known
// only by ARN (e.g. from the Tagging API sweep) are parsed first.
func URL(r model.Resource) (string, bool) {
	n := normalize(r)
	if link, ok := deepLink(n); ok {
		return link, true
	}
	if r.ARN != "" {
		return ARNSearch(r.ARN), false
	}
	return fmt.Sprintf("https://%s.console.aws.amazon.com/console/home?region=%s",
		n.region, n.region), false
}

// FromARN returns a console link for a bare ARN — the spec'd entry point for
// callers that have nothing else.
func FromARN(arn string) (string, bool) {
	return URL(model.Resource{ARN: arn})
}

// ARNSearch returns the console's "go to resource" URL, which resolves an
// ARN to its console page for almost every service.
func ARNSearch(arn string) string {
	return "https://console.aws.amazon.com/go/view?arn=" + url.QueryEscape(arn)
}

// linkInput is a resource reduced to the fields console URLs are built from.
type linkInput struct {
	service string // canonical: ec2, s3, lambda, rds, iam, elbv2, …
	typ     string // lowercase resource type, e.g. "instance", "security-group"
	region  string // never empty: defaults to us-east-1 for global resources
	account string // from the ARN when available (SQS queue URLs need it)
	id      string
	name    string
	arn     string
}

// normalize merges the typed fields with whatever the ARN provides, so both
// rich collector entries and ARN-only Tagging API entries link equally well.
func normalize(r model.Resource) linkInput {
	n := linkInput{
		service: strings.ToLower(r.Service),
		typ:     strings.ToLower(r.Type),
		region:  r.Region,
		id:      r.ID,
		name:    r.Name,
		arn:     r.ARN,
	}
	if r.ARN != "" {
		if parts := strings.SplitN(r.ARN, ":", 6); len(parts) == 6 {
			if n.service == "" {
				n.service = canonicalService(parts[2])
			}
			if n.region == "" {
				n.region = parts[3]
			}
			n.account = parts[4]
			resource := parts[5]
			typ, id := splitResource(resource)
			if n.typ == "" {
				n.typ = typ
			}
			if n.id == "" {
				n.id = id
			}
		}
	}
	if n.region == "" || n.region == "global" {
		n.region = "us-east-1"
	}
	if n.name == "" {
		n.name = n.id
	}
	return n
}

// splitResource splits an ARN resource field ("instance/i-0abc",
// "function:my-fn", "my-queue") into type and ID.
func splitResource(resource string) (typ, id string) {
	if i := strings.IndexByte(resource, '/'); i >= 0 {
		return strings.ToLower(resource[:i]), resource[i+1:]
	}
	if i := strings.IndexByte(resource, ':'); i >= 0 {
		return strings.ToLower(resource[:i]), resource[i+1:]
	}
	return "", resource
}

// canonicalService maps ARN service namespaces to the names the typed
// collectors use.
func canonicalService(s string) string {
	switch strings.ToLower(s) {
	case "elasticloadbalancing":
		return "elbv2"
	case "elasticmapreduce":
		return "emr"
	default:
		return strings.ToLower(s)
	}
}

// lastSegment trims any path from an ID ("service-role/my-role" → "my-role"),
// as console URLs want the bare name.
func lastSegment(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// deepLink builds the type-specific console URL. The patterns cover the 15
// supported services plus the VPC explorer's resource types; anything else
// reports false so the caller can fall back to the ARN search.
func deepLink(n linkInput) (string, bool) {
	q := url.QueryEscape
	ec2Home := fmt.Sprintf("https://%s.console.aws.amazon.com/ec2/home?region=%s", n.region, n.region)
	vpcHome := fmt.Sprintf("https://%s.console.aws.amazon.com/vpc/home?region=%s", n.region, n.region)

	switch n.service {
	case "ec2":
		switch n.typ {
		case "instance":
			return ec2Home + "#InstanceDetails:instanceId=" + q(n.id), true
		case "volume":
			return ec2Home + "#VolumeDetails:volumeId=" + q(n.id), true
		case "snapshot":
			return ec2Home + "#SnapshotDetails:snapshotId=" + q(n.id), true
		case "image", "ami":
			return ec2Home + "#ImageDetails:imageId=" + q(n.id), true
		case "security-group", "securitygroup", "security_group", "sg":
			return ec2Home + "#SecurityGroup:groupId=" + q(n.id), true
		case "network-interface", "eni":
			return ec2Home + "#NetworkInterface:networkInterfaceId=" + q(n.id), true
		case "elastic-ip", "eip", "address", "elastic-ip-allocation":
			return ec2Home + "#ElasticIpDetails:AllocationId=" + q(n.id), true
		case "key-pair":
			return ec2Home + "#KeyPairs:search=" + q(n.id), true
		case "vpc":
			return vpcHome + "#VpcDetails:VpcId=" + q(n.id), true
		case "subnet":
			return vpcHome + "#SubnetDetails:subnetId=" + q(n.id), true
		case "route-table", "routetable":
			return vpcHome + "#RouteTableDetails:RouteTableId=" + q(n.id), true
		case "internet-gateway", "igw":
			return vpcHome + "#InternetGateway:internetGatewayId=" + q(n.id), true
		case "natgateway", "nat-gateway":
			return vpcHome + "#NatGatewayDetails:natGatewayId=" + q(n.id), true
		case "network-acl", "networkacl", "nacl":
			return vpcHome + "#NetworkAclDetails:networkAclId=" + q(n.id), true
		case "vpc-endpoint", "endpoint":
			return vpcHome + "#EndpointDetails:vpcEndpointId=" + q(n.id), true
		case "vpc-peering-connection", "peering":
			return vpcHome + "#PeeringConnectionDetails:VpcPeeringConnectionId=" + q(n.id), true
		}

	case "s3":
		if n.id != "" {
			return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s", q(n.id), n.region), true
		}

	case "lambda":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/lambda/home?region=%s#/functions/%s",
			n.region, n.region, q(lastSegment(n.name))), true

	case "rds":
		switch n.typ {
		case "cluster":
			return fmt.Sprintf("https://%s.console.aws.amazon.com/rds/home?region=%s#database:id=%s;is-cluster=true",
				n.region, n.region, q(n.id)), true
		case "snapshot", "cluster-snapshot":
			return fmt.Sprintf("https://%s.console.aws.amazon.com/rds/home?region=%s#snapshots-list:",
				n.region, n.region), true
		default: // db / instance
			return fmt.Sprintf("https://%s.console.aws.amazon.com/rds/home?region=%s#database:id=%s;is-cluster=false",
				n.region, n.region, q(n.id)), true
		}

	case "dynamodb":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/dynamodbv2/home?region=%s#table?name=%s",
			n.region, n.region, q(lastSegment(n.id))), true

	case "iam":
		switch n.typ {
		case "role":
			return "https://console.aws.amazon.com/iam/home#/roles/" + q(lastSegment(n.id)), true
		case "user":
			return "https://console.aws.amazon.com/iam/home#/users/" + q(lastSegment(n.id)), true
		case "group":
			return "https://console.aws.amazon.com/iam/home#/groups/" + q(lastSegment(n.id)), true
		case "policy":
			if n.arn != "" {
				// The ARN sits in the fragment path, not a query string, so use
				// PathEscape: QueryEscape would turn any space into "+" (a
				// literal plus in a path) and is the wrong encoding here.
				return "https://console.aws.amazon.com/iam/home#/policies/details/" + url.PathEscape(n.arn), true
			}
		}

	case "eks":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/eks/home?region=%s#/clusters/%s",
			n.region, n.region, q(lastSegment(n.id))), true

	case "ecs":
		switch n.typ {
		case "service":
			// ARN id is "<cluster>/<service>" in the new (long) format.
			if cluster, svc, ok := strings.Cut(n.id, "/"); ok {
				return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s/services/%s?region=%s",
					n.region, q(cluster), q(svc), n.region), true
			}
		case "task-definition":
			return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/task-definitions?region=%s",
				n.region, n.region), true
		default: // cluster
			return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters/%s?region=%s",
				n.region, q(lastSegment(n.id)), n.region), true
		}

	case "elbv2":
		// Load balancer and target group ARNs embed random suffixes; the
		// search view is the reliable deep link. Searching by ARN pins one
		// resource; a bare name still narrows the list to it.
		needle := n.arn
		if needle == "" {
			needle = n.id
		}
		if needle != "" {
			anchor := "#LoadBalancers:search="
			if strings.HasPrefix(n.typ, "targetgroup") || strings.HasPrefix(n.typ, "target-group") {
				anchor = "#TargetGroups:search="
			}
			return ec2Home + anchor + q(needle), true
		}

	case "sqs":
		// The console addresses queues by their URL-encoded queue URL.
		if n.account != "" && n.id != "" {
			queueURL := fmt.Sprintf("https://sqs.%s.amazonaws.com/%s/%s", n.region, n.account, lastSegment(n.id))
			return fmt.Sprintf("https://%s.console.aws.amazon.com/sqs/v3/home?region=%s#/queues/%s",
				n.region, n.region, q(queueURL)), true
		}

	case "sns":
		if n.arn != "" {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/sns/v3/home?region=%s#/topic/%s",
				n.region, n.region, q(n.arn)), true
		}

	case "secretsmanager":
		// Secret ARNs end in a random suffix; the list-with-search link is
		// the dependable one.
		return fmt.Sprintf("https://%s.console.aws.amazon.com/secretsmanager/listsecrets?region=%s&search=%s",
			n.region, n.region, q(lastSegment(n.name))), true

	case "cloudwatch":
		if n.typ == "alarm" {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s#alarmsV2:alarm/%s",
				n.region, n.region, q(lastSegment(n.id))), true
		}

	case "logs":
		// Log group names contain "/", which this console view expects
		// double-encoded ("%2F" → "$252F").
		group := lastSegmentAfter(n.arn, "log-group:")
		if group == "" {
			group = n.id
		}
		if group != "" {
			return LogGroupURL(n.region, group), true
		}

	case "route53":
		if n.typ == "hosted-zone" {
			return "https://us-east-1.console.aws.amazon.com/route53/v2/hostedzones#ListRecordSets/" + q(lastSegment(n.id)), true
		}

	case "emr":
		emrHome := fmt.Sprintf("https://%s.console.aws.amazon.com/emr/home?region=%s", n.region, n.region)
		switch n.typ {
		case "studio":
			return emrHome + "#/studio/" + q(lastSegment(n.id)), true
		case "notebook", "editor":
			return emrHome + "#/notebooks/" + q(lastSegment(n.id)), true
		default:
			// Clusters (and steps, which link to their cluster page).
			return emrHome + "#/clusterDetails/" + q(lastSegment(n.id)), true
		}

	case "acm":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/acm/home?region=%s#/certificates/%s",
			n.region, n.region, q(lastSegment(n.id))), true

	case "glue":
		glueHome := fmt.Sprintf("https://%s.console.aws.amazon.com/glue/home?region=%s", n.region, n.region)
		name := lastSegment(n.id)
		switch n.typ {
		case "job":
			// Jobs open in the Glue Studio visual/script editor.
			return fmt.Sprintf("https://%s.console.aws.amazon.com/gluestudio/home?region=%s#/editor/job/%s/details",
				n.region, n.region, q(name)), true
		case "crawler":
			return glueHome + "#/v2/data-catalog/crawlers/view/" + q(name), true
		case "database":
			return glueHome + "#/v2/data-catalog/databases/view/" + q(name), true
		case "trigger":
			return glueHome + "#/v2/etl-configuration/triggers/view/" + q(name), true
		case "workflow":
			return glueHome + "#/v2/etl-configuration/workflows/view/" + q(name), true
		case "connection":
			return glueHome + "#/v2/data-catalog/connections/view/" + q(name), true
		}
	}

	return "", false
}

// lastSegmentAfter returns the substring after the first occurrence of sep,
// or "" when sep is absent.
func lastSegmentAfter(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return ""
}

// LogGroupURL returns the CloudWatch console URL for a log group. Group
// names contain "/", which this console view expects double-encoded
// ("%2F" → "$252F").
func LogGroupURL(region, group string) string {
	if region == "" || region == "global" {
		region = "us-east-1"
	}
	enc := strings.ReplaceAll(url.PathEscape(group), "%", "$25")
	return fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s#logsV2:log-groups/log-group/%s",
		region, region, enc)
}

// S3BucketURL returns the console URL for a bucket, optionally scoped to a
// prefix (folder).
func S3BucketURL(bucket, prefix, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	u := fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s", url.QueryEscape(bucket), region)
	if prefix != "" {
		u += "&prefix=" + url.QueryEscape(prefix)
	}
	return u
}

// S3ObjectURL returns the console URL for a single object.
func S3ObjectURL(bucket, key, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/object/%s?region=%s&prefix=%s",
		url.QueryEscape(bucket), region, url.QueryEscape(key))
}
