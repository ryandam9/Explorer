package awsutil

import (
	"fmt"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// ConsoleURL generates the AWS console link for a given resource.
func ConsoleURL(r model.Resource) string {
	region := r.Region
	if region == "" || region == "global" {
		region = "us-east-1"
	}

	switch strings.ToLower(r.Service) {
	case "ec2":
		if strings.EqualFold(r.Type, "instance") {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/ec2/v2/home?region=%s#InstanceDetails:instanceId=%s", region, region, r.ID)
		}
		if strings.EqualFold(r.Type, "vpc") {
			return fmt.Sprintf("https://%s.console.aws.amazon.com/vpc/home?region=%s#vpcs:VpcId=%s", region, region, r.ID)
		}
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ec2/v2/home?region=%s", region, region)

	case "s3":
		return fmt.Sprintf("https://s3.console.aws.amazon.com/s3/buckets/%s?region=%s", r.ID, region)

	case "rds":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/rds/home?region=%s#database:id=%s", region, region, r.ID)

	case "lambda":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/lambda/home?region=%s#/functions/%s", region, region, r.Name)

	case "eks":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/eks/home?region=%s#/clusters/%s", region, region, r.ID)

	case "ecs":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/ecs/v2/clusters?region=%s", region, region)

	case "iam":
		if strings.EqualFold(r.Type, "role") {
			return fmt.Sprintf("https://console.aws.amazon.com/iam/home#/roles/%s", r.Name)
		}
		return "https://console.aws.amazon.com/iam/home"

	case "cloudwatch":
		return fmt.Sprintf("https://%s.console.aws.amazon.com/cloudwatch/home?region=%s#alarmsV2:", region, region)
	}

	// General fallback console URL
	return fmt.Sprintf("https://%s.console.aws.amazon.com/console/home?region=%s", region, region)
}

// AWSCLICommand returns the copy-pasteable AWS CLI command to describe/get the resource.
func AWSCLICommand(r model.Resource) string {
	region := r.Region
	regionFlag := ""
	if region != "" && region != "global" {
		regionFlag = " --region " + region
	}

	switch strings.ToLower(r.Service) {
	case "ec2":
		if strings.EqualFold(r.Type, "instance") {
			return fmt.Sprintf("aws ec2 describe-instances --instance-ids %s%s", r.ID, regionFlag)
		}
		if strings.EqualFold(r.Type, "vpc") {
			return fmt.Sprintf("aws ec2 describe-vpcs --vpc-ids %s%s", r.ID, regionFlag)
		}

	case "s3":
		return fmt.Sprintf("aws s3 ls s3://%s%s", r.ID, regionFlag)

	case "rds":
		return fmt.Sprintf("aws rds describe-db-instances --db-instance-identifier %s%s", r.ID, regionFlag)

	case "lambda":
		return fmt.Sprintf("aws lambda get-function --function-name %s%s", r.Name, regionFlag)

	case "eks":
		return fmt.Sprintf("aws eks describe-cluster --name %s%s", r.ID, regionFlag)

	case "sqs":
		return fmt.Sprintf("aws sqs get-queue-attributes --queue-url %s --attribute-names All%s", r.ID, regionFlag)

	case "sns":
		return fmt.Sprintf("aws sns get-topic-attributes --topic-arn %s%s", r.ARN, regionFlag)
	}

	return fmt.Sprintf("aws %s describe-%ss --id %s%s", strings.ToLower(r.Service), strings.ToLower(r.Type), r.ID, regionFlag)
}
