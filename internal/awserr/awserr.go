package awserr

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aws/smithy-go"
)

// authErrorCodes are AWS error codes that indicate insufficient IAM privileges.
var authErrorCodes = map[string]bool{
	"AccessDenied":          true,
	"AccessDeniedException": true,
	"UnauthorizedOperation": true,
	"AuthorizationError":    true,
	"Forbidden":             true,
}

// servicePermissions maps collector service names to the IAM actions needed to list resources.
var servicePermissions = map[string][]string{
	"ec2":            {"ec2:DescribeInstances", "ec2:DescribeVpcs"},
	"s3":             {"s3:ListBuckets"},
	"rds":            {"rds:DescribeDBInstances", "rds:DescribeDBClusters"},
	"iam":            {"iam:ListUsers", "iam:ListRoles", "iam:ListPolicies", "iam:ListGroups"},
	"dynamodb":       {"dynamodb:ListTables"},
	"lambda":         {"lambda:ListFunctions"},
	"emr":            {"elasticmapreduce:ListClusters"},
	"ecs":            {"ecs:ListClusters", "ecs:ListServices", "ecs:ListTasks"},
	"eks":            {"eks:ListClusters"},
	"elbv2":          {"elasticloadbalancing:DescribeLoadBalancers"},
	"secretsmanager": {"secretsmanager:ListSecrets"},
	"sqs":            {"sqs:ListQueues"},
	"sns":            {"sns:ListTopics"},
	"cloudwatch":     {"cloudwatch:DescribeAlarms"},
	"cloudfront":     {"cloudfront:ListDistributions"},
	"route53":        {"route53:ListHostedZones"},
	"apigateway":     {"apigateway:GET"},
	"stepfunctions":  {"states:ListStateMachines"},
	"eventbridge":    {"events:ListEventBuses", "events:ListRules"},
	"elasticache":    {"elasticache:DescribeCacheClusters"},
	"efs":            {"elasticfilesystem:DescribeFileSystems"},
	"kinesis":        {"kinesis:ListStreams"},
	"redshift":       {"redshift:DescribeClusters"},
	"kms":            {"kms:ListKeys", "kms:DescribeKey"},
	"ecr":            {"ecr:DescribeRepositories"},
	"acm":            {"acm:ListCertificates"},
	"cloudformation": {"cloudformation:DescribeStacks"},
	"glue":           {"glue:GetDatabases", "glue:GetJobs", "glue:GetCrawlers"},
	"athena":         {"athena:ListWorkGroups"},
	"resourcegroups": {"tag:GetResources"},
}

// IsAuthError reports whether err is an AWS insufficient-privilege error.
// It unwraps the full error chain, so wrapped errors (e.g. fmt.Errorf("...: %w", awsErr)) work too.
func IsAuthError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return authErrorCodes[apiErr.ErrorCode()]
}

// FriendlyMessage returns a human-readable privilege error message.
// It first tries to extract the specific IAM action from the AWS error message,
// then falls back to a per-service hint table.
func FriendlyMessage(err error, service string) string {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return err.Error()
	}

	msg := apiErr.ErrorMessage()

	// AWS messages often contain "is not authorized to perform: <action> on resource: ..."
	const marker = "is not authorized to perform: "
	if idx := strings.Index(msg, marker); idx != -1 {
		rest := msg[idx+len(marker):]
		perm := rest
		if end := strings.IndexAny(perm, " \n\t"); end != -1 {
			perm = perm[:end]
		}
		return fmt.Sprintf("Insufficient privileges — required IAM permission: %s", perm)
	}

	// Fall back to service-specific hints
	if perms, ok := servicePermissions[service]; ok {
		return fmt.Sprintf("Insufficient privileges — to list %s resources, grant: %s",
			strings.ToUpper(service), strings.Join(perms, ", "))
	}

	return fmt.Sprintf("Insufficient privileges (%s)", apiErr.ErrorCode())
}
