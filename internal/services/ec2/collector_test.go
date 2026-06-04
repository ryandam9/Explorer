package ec2

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/user/aws_explorer/internal/services"
)

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
		Tags: []types.Tag{
			{Key: aws.String("Name"), Value: aws.String("my-server")},
			{Key: aws.String("env"), Value: aws.String("prod")},
		},
	}

	res := c.mapInstance("us-east-1", instance, services.DetailLevelSummary)

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

	res := c.mapInstance("eu-west-1", instance, services.DetailLevelDetailed)

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

	res := c.mapVpc("us-west-2", vpc, services.DetailLevelSummary)

	if res.ID != "vpc-0abc123" {
		t.Errorf("ID = %q, want %q", res.ID, "vpc-0abc123")
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

	res := c.mapVpc("ap-southeast-1", vpc, services.DetailLevelSummary)

	if res.Name != "" {
		t.Errorf("expected empty name, got %q", res.Name)
	}
}
