package ec2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/user/aws_explorer/internal/awsutil"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

// Collector implements the services.Collector interface for EC2.
type Collector struct{}

// NewCollector returns a new EC2 Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "ec2"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := ec2.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	// 1. Collect Instances
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe EC2 instances: %w", err)
		}

		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				resources = append(resources, c.mapInstance(input.Region, input.AccountID, instance, input.DetailLevel))
			}
		}
	}

	// 2. Collect VPCs
	vpcPaginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for vpcPaginator.HasMorePages() {
		page, err := vpcPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe VPCs: %w", err)
		}
		for _, vpc := range page.Vpcs {
			resources = append(resources, c.mapVpc(input.Region, input.AccountID, vpc, input.DetailLevel))
		}
	}

	return resources, nil
}

func (c *Collector) mapInstance(region, account string, instance types.Instance, detail services.DetailLevel) model.Resource {
	id := aws.ToString(instance.InstanceId)
	state := ""
	if instance.State != nil {
		state = string(instance.State.Name)
	}
	iType := string(instance.InstanceType)

	az := ""
	if instance.Placement != nil {
		az = aws.ToString(instance.Placement.AvailabilityZone)
	}

	name := awsutil.EC2TagName(instance.Tags)
	tags := awsutil.EC2TagsToMap(instance.Tags)

	res := model.Resource{
		Service:   "ec2",
		Type:      "instance",
		Region:    region,
		AZ:        az,
		AccountID: account,
		ID:        id,
		Name:      name,
		ARN:       awsutil.EC2ARN(region, account, "instance", id),
		State:     state,
		Tags:      tags,
		Summary: map[string]string{
			"instanceType": iType,
			"privateIp":    aws.ToString(instance.PrivateIpAddress),
			"publicIp":     aws.ToString(instance.PublicIpAddress),
			"vpcId":        aws.ToString(instance.VpcId),
			"subnetId":     aws.ToString(instance.SubnetId),
		},
	}

	if instance.LaunchTime != nil {
		res.CreatedAt = instance.LaunchTime
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"imageId": aws.ToString(instance.ImageId),
		}
	}

	return res
}

func (c *Collector) mapVpc(region, account string, vpc types.Vpc, detail services.DetailLevel) model.Resource {
	id := aws.ToString(vpc.VpcId)
	state := string(vpc.State)

	name := awsutil.EC2TagName(vpc.Tags)
	tags := awsutil.EC2TagsToMap(vpc.Tags)

	res := model.Resource{
		Service:   "ec2",
		Type:      "vpc",
		Region:    region,
		AccountID: account,
		ID:        id,
		Name:      name,
		ARN:       awsutil.EC2ARN(region, account, "vpc", id),
		State:     state,
		Tags:      tags,
		Summary: map[string]string{
			"cidrBlock": aws.ToString(vpc.CidrBlock),
			"isDefault": fmt.Sprintf("%t", aws.ToBool(vpc.IsDefault)),
		},
	}
	return res
}
