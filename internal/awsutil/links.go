package awsutil

import (
	"fmt"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

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
