package ec2

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// ec2API is the subset of the EC2 client used by the collector. Splitting it
// out lets each resource family be exercised with a fake client in tests.
type ec2API interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeVpcsAPIClient
	ec2.DescribeSubnetsAPIClient
	ec2.DescribeSecurityGroupsAPIClient
	ec2.DescribeVolumesAPIClient
	ec2.DescribeNetworkInterfacesAPIClient
}

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
	return c.collect(ctx, ec2.NewFromConfig(input.AWSConfig), input)
}

// collect gathers every EC2 resource family. Each family is collected
// independently: a failure in one (e.g. a denied DescribeVpcs) degrades only
// that family — already-collected resources are kept and the remaining
// families are still queried. All family errors are joined and returned so the
// engine records them as a partial result rather than aborting the region.
func (c *Collector) collect(ctx context.Context, api ec2API, input services.CollectInput) ([]model.Resource, error) {
	var resources []model.Resource
	var errs []error

	collectors := []func(context.Context, ec2API, services.CollectInput, []model.Resource) ([]model.Resource, error){
		c.collectInstances,
		c.collectVPCs,
		c.collectSubnets,
		c.collectSecurityGroups,
		c.collectVolumes,
		c.collectNetworkInterfaces,
	}
	for _, collect := range collectors {
		var err error
		if resources, err = collect(ctx, api, input, resources); err != nil {
			errs = append(errs, err)
		}
	}

	return resources, errors.Join(errs...)
}

func (c *Collector) collectInstances(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeInstancesPaginator(api, &ec2.DescribeInstancesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe EC2 instances: %w", err)
		}
		var batch []model.Resource
		for _, reservation := range page.Reservations {
			for _, instance := range reservation.Instances {
				batch = append(batch, c.mapInstance(input.Region, input.AccountID, instance, input.DetailLevel))
			}
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectVPCs(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeVpcsPaginator(api, &ec2.DescribeVpcsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe VPCs: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Vpcs))
		for _, vpc := range page.Vpcs {
			batch = append(batch, c.mapVpc(input.Region, input.AccountID, vpc, input.DetailLevel))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectSubnets(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeSubnetsPaginator(api, &ec2.DescribeSubnetsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe subnets: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Subnets))
		for _, subnet := range page.Subnets {
			batch = append(batch, c.mapSubnet(input.Region, input.AccountID, subnet))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectSecurityGroups(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeSecurityGroupsPaginator(api, &ec2.DescribeSecurityGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe security groups: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.SecurityGroups))
		for _, sg := range page.SecurityGroups {
			batch = append(batch, c.mapSecurityGroup(input.Region, input.AccountID, sg))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectVolumes(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeVolumesPaginator(api, &ec2.DescribeVolumesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe volumes: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Volumes))
		for _, vol := range page.Volumes {
			batch = append(batch, c.mapVolume(input.Region, input.AccountID, vol))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectNetworkInterfaces(ctx context.Context, api ec2API, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := ec2.NewDescribeNetworkInterfacesPaginator(api, &ec2.DescribeNetworkInterfacesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to describe network interfaces: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.NetworkInterfaces))
		for _, eni := range page.NetworkInterfaces {
			batch = append(batch, c.mapNetworkInterface(input.Region, input.AccountID, eni))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
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
