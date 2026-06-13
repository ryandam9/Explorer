package ec2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
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

		var batch []model.Resource
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				batch = append(batch, c.mapInstance(input.Region, input.AccountID, instance, input.DetailLevel))
			}
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 2. Collect VPCs
	vpcPaginator := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for vpcPaginator.HasMorePages() {
		page, err := vpcPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe VPCs: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Vpcs))
		for _, vpc := range page.Vpcs {
			batch = append(batch, c.mapVpc(input.Region, input.AccountID, vpc, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 3. Collect Subnets
	subnetPaginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for subnetPaginator.HasMorePages() {
		page, err := subnetPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe subnets: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Subnets))
		for _, subnet := range page.Subnets {
			batch = append(batch, c.mapSubnet(input.Region, input.AccountID, subnet))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 4. Collect Security Groups
	sgPaginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})
	for sgPaginator.HasMorePages() {
		page, err := sgPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe security groups: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.SecurityGroups))
		for _, sg := range page.SecurityGroups {
			batch = append(batch, c.mapSecurityGroup(input.Region, input.AccountID, sg))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 5. Collect EBS Volumes
	volPaginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for volPaginator.HasMorePages() {
		page, err := volPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe volumes: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Volumes))
		for _, vol := range page.Volumes {
			batch = append(batch, c.mapVolume(input.Region, input.AccountID, vol))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 6. Collect Network Interfaces (ENIs)
	eniPaginator := ec2.NewDescribeNetworkInterfacesPaginator(client, &ec2.DescribeNetworkInterfacesInput{})
	for eniPaginator.HasMorePages() {
		page, err := eniPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe network interfaces: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.NetworkInterfaces))
		for _, eni := range page.NetworkInterfaces {
			batch = append(batch, c.mapNetworkInterface(input.Region, input.AccountID, eni))
		}
		resources = input.EmitOrAppend(resources, batch)
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

func (c *Collector) mapSubnet(region, account string, subnet types.Subnet) model.Resource {
	id := aws.ToString(subnet.SubnetId)
	az := aws.ToString(subnet.AvailabilityZone)
	return model.Resource{
		Service:   "ec2",
		Type:      "subnet",
		Region:    region,
		AZ:        az,
		AccountID: account,
		ID:        id,
		Name:      awsutil.EC2TagName(subnet.Tags),
		ARN:       awsutil.EC2ARN(region, account, "subnet", id),
		State:     string(subnet.State),
		Tags:      awsutil.EC2TagsToMap(subnet.Tags),
		Summary: map[string]string{
			"cidrBlock":    aws.ToString(subnet.CidrBlock),
			"vpcId":        aws.ToString(subnet.VpcId),
			"availableIps": fmt.Sprintf("%d", aws.ToInt32(subnet.AvailableIpAddressCount)),
			"mapPublicIp":  fmt.Sprintf("%t", aws.ToBool(subnet.MapPublicIpOnLaunch)),
		},
	}
}

func (c *Collector) mapSecurityGroup(region, account string, sg types.SecurityGroup) model.Resource {
	id := aws.ToString(sg.GroupId)
	name := aws.ToString(sg.GroupName)
	return model.Resource{
		Service:   "ec2",
		Type:      "security-group",
		Region:    region,
		AccountID: account,
		ID:        id,
		Name:      name,
		ARN:       awsutil.EC2ARN(region, account, "security-group", id),
		Tags:      awsutil.EC2TagsToMap(sg.Tags),
		Summary: map[string]string{
			"groupName":    name,
			"vpcId":        aws.ToString(sg.VpcId),
			"description":  aws.ToString(sg.Description),
			"ingressRules": fmt.Sprintf("%d", len(sg.IpPermissions)),
			"egressRules":  fmt.Sprintf("%d", len(sg.IpPermissionsEgress)),
		},
	}
}

func (c *Collector) mapVolume(region, account string, vol types.Volume) model.Resource {
	id := aws.ToString(vol.VolumeId)
	az := aws.ToString(vol.AvailabilityZone)

	attached := ""
	if len(vol.Attachments) > 0 {
		attached = aws.ToString(vol.Attachments[0].InstanceId)
	}

	res := model.Resource{
		Service:   "ec2",
		Type:      "volume",
		Region:    region,
		AZ:        az,
		AccountID: account,
		ID:        id,
		Name:      awsutil.EC2TagName(vol.Tags),
		ARN:       awsutil.EC2ARN(region, account, "volume", id),
		State:     string(vol.State),
		Tags:      awsutil.EC2TagsToMap(vol.Tags),
		Summary: map[string]string{
			"volumeType": string(vol.VolumeType),
			"sizeGiB":    fmt.Sprintf("%d", aws.ToInt32(vol.Size)),
			"encrypted":  fmt.Sprintf("%t", aws.ToBool(vol.Encrypted)),
			"attachedTo": attached,
		},
	}
	if vol.CreateTime != nil {
		res.CreatedAt = vol.CreateTime
	}
	return res
}

func (c *Collector) mapNetworkInterface(region, account string, eni types.NetworkInterface) model.Resource {
	id := aws.ToString(eni.NetworkInterfaceId)
	az := aws.ToString(eni.AvailabilityZone)

	attachedTo := ""
	if eni.Attachment != nil {
		attachedTo = aws.ToString(eni.Attachment.InstanceId)
	}

	// ENIs carry tags under TagSet rather than Tags.
	name := awsutil.EC2TagName(eni.TagSet)

	return model.Resource{
		Service:   "ec2",
		Type:      "network-interface",
		Region:    region,
		AZ:        az,
		AccountID: account,
		ID:        id,
		Name:      name,
		ARN:       awsutil.EC2ARN(region, account, "network-interface", id),
		State:     string(eni.Status),
		Tags:      awsutil.EC2TagsToMap(eni.TagSet),
		Summary: map[string]string{
			"privateIp":     aws.ToString(eni.PrivateIpAddress),
			"vpcId":         aws.ToString(eni.VpcId),
			"subnetId":      aws.ToString(eni.SubnetId),
			"interfaceType": string(eni.InterfaceType),
			"attachedTo":    attachedTo,
		},
	}
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
