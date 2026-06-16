package ec2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// fakeEC2 implements ec2API. Each Describe* call returns its configured error
// (non-nil => that family fails) and otherwise a single resource so successful
// families are observable in the output.
type fakeEC2 struct {
	instErr, vpcErr, subnetErr, sgErr, volErr, eniErr error
}

func (f fakeEC2) DescribeInstances(context.Context, *ec2.DescribeInstancesInput, ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.instErr != nil {
		return nil, f.instErr
	}
	return &ec2.DescribeInstancesOutput{Reservations: []types.Reservation{{Instances: []types.Instance{{InstanceId: aws.String("i-1")}}}}}, nil
}

func (f fakeEC2) DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if f.vpcErr != nil {
		return nil, f.vpcErr
	}
	return &ec2.DescribeVpcsOutput{Vpcs: []types.Vpc{{VpcId: aws.String("vpc-1")}}}, nil
}

func (f fakeEC2) DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if f.subnetErr != nil {
		return nil, f.subnetErr
	}
	return &ec2.DescribeSubnetsOutput{Subnets: []types.Subnet{{SubnetId: aws.String("subnet-1")}}}, nil
}

func (f fakeEC2) DescribeSecurityGroups(context.Context, *ec2.DescribeSecurityGroupsInput, ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if f.sgErr != nil {
		return nil, f.sgErr
	}
	return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []types.SecurityGroup{{GroupId: aws.String("sg-1")}}}, nil
}

func (f fakeEC2) DescribeVolumes(context.Context, *ec2.DescribeVolumesInput, ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	if f.volErr != nil {
		return nil, f.volErr
	}
	return &ec2.DescribeVolumesOutput{Volumes: []types.Volume{{VolumeId: aws.String("vol-1")}}}, nil
}

func (f fakeEC2) DescribeNetworkInterfaces(context.Context, *ec2.DescribeNetworkInterfacesInput, ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error) {
	if f.eniErr != nil {
		return nil, f.eniErr
	}
	return &ec2.DescribeNetworkInterfacesOutput{NetworkInterfaces: []types.NetworkInterface{{NetworkInterfaceId: aws.String("eni-1")}}}, nil
}

func TestCollect_PartialFailureKeepsOtherFamilies(t *testing.T) {
	c := NewCollector()
	// VPCs and security groups are denied; every other family succeeds.
	api := fakeEC2{
		vpcErr: errors.New("AccessDenied: DescribeVpcs"),
		sgErr:  errors.New("AccessDenied: DescribeSecurityGroups"),
	}

	resources, err := c.collect(context.Background(), api, services.CollectInput{Region: "us-east-1", AccountID: "123456789012"})

	if err == nil {
		t.Fatal("expected a joined error for the failed families")
	}
	if msg := err.Error(); !strings.Contains(msg, "VPCs") || !strings.Contains(msg, "security groups") {
		t.Errorf("joined error should name both failed families, got: %v", msg)
	}

	got := map[string]bool{}
	for _, r := range resources {
		got[r.Type] = true
	}
	for _, want := range []string{"instance", "subnet", "volume", "network-interface"} {
		if !got[want] {
			t.Errorf("expected %q to still be collected despite VPC/SG failures; got types %v", want, got)
		}
	}
	if got["vpc"] || got["security-group"] {
		t.Errorf("failed families should yield no resources; got types %v", got)
	}
}

func TestMapInstance_BasicFields(t *testing.T) {
	c := NewCollector()
	launch := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	instance := types.Instance{
		InstanceId:       aws.String("i-0abc123"),
		InstanceType:     types.InstanceTypeT3Micro,
		State:            &types.InstanceState{Name: types.InstanceStateNameRunning},
		PrivateIpAddress: aws.String("10.0.0.1"),
		PublicIpAddress:  aws.String("1.2.3.4"),
		VpcId:            aws.String("vpc-abc"),
		SubnetId:         aws.String("subnet-abc"),
		LaunchTime:       &launch,
		Placement:        &types.Placement{AvailabilityZone: aws.String("us-east-1a")},
		Tags: []types.Tag{
			{Key: aws.String("Name"), Value: aws.String("my-server")},
			{Key: aws.String("env"), Value: aws.String("prod")},
		},
	}

	res := c.mapInstance("us-east-1", "123456789012", instance, services.DetailLevelSummary)

	if res.ID != "i-0abc123" {
		t.Errorf("ID = %q, want %q", res.ID, "i-0abc123")
	}
	if res.Name != "my-server" {
		t.Errorf("Name = %q, want %q", res.Name, "my-server")
	}
	if res.State != "running" {
		t.Errorf("State = %q, want %q", res.State, "running")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.AZ != "us-east-1a" {
		t.Errorf("AZ = %q, want %q", res.AZ, "us-east-1a")
	}
	if want := "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc123"; res.ARN != want {
		t.Errorf("ARN = %q, want %q", res.ARN, want)
	}
	if res.Tags["env"] != "prod" {
		t.Errorf("Tags[env] = %q, want %q", res.Tags["env"], "prod")
	}
	if res.Summary["instanceType"] != "t3.micro" {
		t.Errorf("Summary[instanceType] = %q, want %q", res.Summary["instanceType"], "t3.micro")
	}
	if res.Summary["privateIp"] != "10.0.0.1" {
		t.Errorf("Summary[privateIp] = %q", res.Summary["privateIp"])
	}
	if !res.CreatedAt.Equal(launch) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, launch)
	}
	if res.Details != nil {
		t.Error("expected no Details at summary level")
	}
}

func TestMapInstance_DetailLevel(t *testing.T) {
	c := NewCollector()
	instance := types.Instance{
		InstanceId:   aws.String("i-detail"),
		InstanceType: types.InstanceTypeT3Micro,
		State:        &types.InstanceState{Name: types.InstanceStateNameStopped},
		ImageId:      aws.String("ami-0abc"),
	}

	res := c.mapInstance("eu-west-1", "123456789012", instance, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["imageId"] != "ami-0abc" {
		t.Errorf("Details[imageId] = %v, want %q", res.Details["imageId"], "ami-0abc")
	}
}

func TestMapVpc_BasicFields(t *testing.T) {
	c := NewCollector()
	vpc := types.Vpc{
		VpcId:     aws.String("vpc-0abc123"),
		State:     types.VpcStateAvailable,
		CidrBlock: aws.String("10.0.0.0/16"),
		IsDefault: aws.Bool(true),
		Tags: []types.Tag{
			{Key: aws.String("Name"), Value: aws.String("main-vpc")},
		},
	}

	res := c.mapVpc("us-west-2", "123456789012", vpc, services.DetailLevelSummary)

	if res.ID != "vpc-0abc123" {
		t.Errorf("ID = %q, want %q", res.ID, "vpc-0abc123")
	}
	if want := "arn:aws:ec2:us-west-2:123456789012:vpc/vpc-0abc123"; res.ARN != want {
		t.Errorf("ARN = %q, want %q", res.ARN, want)
	}
	if res.Name != "main-vpc" {
		t.Errorf("Name = %q, want %q", res.Name, "main-vpc")
	}
	if res.State != "available" {
		t.Errorf("State = %q, want %q", res.State, "available")
	}
	if res.Summary["cidrBlock"] != "10.0.0.0/16" {
		t.Errorf("Summary[cidrBlock] = %q", res.Summary["cidrBlock"])
	}
	if res.Summary["isDefault"] != "true" {
		t.Errorf("Summary[isDefault] = %q, want %q", res.Summary["isDefault"], "true")
	}
}

func TestMapVpc_NoNameTag(t *testing.T) {
	c := NewCollector()
	vpc := types.Vpc{
		VpcId:     aws.String("vpc-noname"),
		State:     types.VpcStateAvailable,
		CidrBlock: aws.String("172.16.0.0/12"),
		IsDefault: aws.Bool(false),
	}

	res := c.mapVpc("ap-southeast-1", "123456789012", vpc, services.DetailLevelSummary)

	if res.Name != "" {
		t.Errorf("expected empty name, got %q", res.Name)
	}
}

func TestMapSubnet(t *testing.T) {
	c := NewCollector()
	subnet := types.Subnet{
		SubnetId:                aws.String("subnet-0abc"),
		VpcId:                   aws.String("vpc-1"),
		AvailabilityZone:        aws.String("us-east-1a"),
		CidrBlock:               aws.String("10.0.1.0/24"),
		State:                   types.SubnetStateAvailable,
		AvailableIpAddressCount: aws.Int32(250),
		MapPublicIpOnLaunch:     aws.Bool(true),
		Tags:                    []types.Tag{{Key: aws.String("Name"), Value: aws.String("public-a")}},
	}
	res := c.mapSubnet("us-east-1", "123456789012", subnet)

	if res.Type != "subnet" || res.ID != "subnet-0abc" {
		t.Errorf("Type/ID = %q/%q", res.Type, res.ID)
	}
	if res.Name != "public-a" {
		t.Errorf("Name = %q, want public-a", res.Name)
	}
	if res.AZ != "us-east-1a" {
		t.Errorf("AZ = %q", res.AZ)
	}
	if res.ARN != "arn:aws:ec2:us-east-1:123456789012:subnet/subnet-0abc" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.State != "available" {
		t.Errorf("State = %q", res.State)
	}
	if res.Summary["cidrBlock"] != "10.0.1.0/24" || res.Summary["availableIps"] != "250" {
		t.Errorf("Summary = %v", res.Summary)
	}
}

func TestMapSecurityGroup(t *testing.T) {
	c := NewCollector()
	sg := types.SecurityGroup{
		GroupId:     aws.String("sg-0abc"),
		GroupName:   aws.String("web-sg"),
		VpcId:       aws.String("vpc-1"),
		Description: aws.String("web tier"),
		IpPermissions: []types.IpPermission{
			{}, {}, // 2 ingress rules
		},
		IpPermissionsEgress: []types.IpPermission{{}},
	}
	res := c.mapSecurityGroup("us-east-1", "123456789012", sg)

	if res.Type != "security-group" || res.ID != "sg-0abc" || res.Name != "web-sg" {
		t.Errorf("Type/ID/Name = %q/%q/%q", res.Type, res.ID, res.Name)
	}
	if res.ARN != "arn:aws:ec2:us-east-1:123456789012:security-group/sg-0abc" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["ingressRules"] != "2" || res.Summary["egressRules"] != "1" {
		t.Errorf("rule counts = %v", res.Summary)
	}
}

func TestMapVolume(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	vol := types.Volume{
		VolumeId:         aws.String("vol-0abc"),
		AvailabilityZone: aws.String("us-east-1b"),
		VolumeType:       types.VolumeTypeGp3,
		Size:             aws.Int32(100),
		Encrypted:        aws.Bool(true),
		State:            types.VolumeStateInUse,
		CreateTime:       &created,
		Attachments:      []types.VolumeAttachment{{InstanceId: aws.String("i-9xyz")}},
	}
	res := c.mapVolume("us-east-1", "123456789012", vol)

	if res.Type != "volume" || res.ID != "vol-0abc" {
		t.Errorf("Type/ID = %q/%q", res.Type, res.ID)
	}
	if res.ARN != "arn:aws:ec2:us-east-1:123456789012:volume/vol-0abc" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["volumeType"] != "gp3" || res.Summary["sizeGiB"] != "100" {
		t.Errorf("Summary = %v", res.Summary)
	}
	if res.Summary["attachedTo"] != "i-9xyz" {
		t.Errorf("attachedTo = %q", res.Summary["attachedTo"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v", res.CreatedAt)
	}
}

func TestMapNetworkInterface(t *testing.T) {
	c := NewCollector()
	eni := types.NetworkInterface{
		NetworkInterfaceId: aws.String("eni-0abc"),
		AvailabilityZone:   aws.String("us-east-1a"),
		PrivateIpAddress:   aws.String("10.0.1.5"),
		VpcId:              aws.String("vpc-1"),
		SubnetId:           aws.String("subnet-1"),
		Status:             types.NetworkInterfaceStatusInUse,
		InterfaceType:      types.NetworkInterfaceTypeInterface,
		Attachment:         &types.NetworkInterfaceAttachment{InstanceId: aws.String("i-9xyz")},
		TagSet:             []types.Tag{{Key: aws.String("Name"), Value: aws.String("primary")}},
	}
	res := c.mapNetworkInterface("us-east-1", "123456789012", eni)

	if res.Type != "network-interface" || res.ID != "eni-0abc" {
		t.Errorf("Type/ID = %q/%q", res.Type, res.ID)
	}
	if res.Name != "primary" {
		t.Errorf("Name = %q, want primary (from TagSet)", res.Name)
	}
	if res.ARN != "arn:aws:ec2:us-east-1:123456789012:network-interface/eni-0abc" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["privateIp"] != "10.0.1.5" || res.Summary["attachedTo"] != "i-9xyz" {
		t.Errorf("Summary = %v", res.Summary)
	}
}
