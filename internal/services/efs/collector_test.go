package efs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "efs" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapFileSystem(t *testing.T) {
	res := NewCollector().mapFileSystem(types.FileSystemDescription{
		FileSystemId:         aws.String("fs-123"),
		FileSystemArn:        aws.String("arn:aws:elasticfilesystem:us-east-1:1:file-system/fs-123"),
		Name:                 aws.String("shared"),
		LifeCycleState:       types.LifeCycleStateAvailable,
		NumberOfMountTargets: 3,
	}, "us-east-1")
	if res.Service != "efs" || res.Type != "fileSystem" {
		t.Fatalf("service/type = %q/%q", res.Service, res.Type)
	}
	if res.Name != "shared" || res.State != "available" || res.Summary["mountTargets"] != "3" {
		t.Errorf("unexpected mapping: %+v", res)
	}
}

func TestMapFileSystem_NoNameFallsBackToID(t *testing.T) {
	res := NewCollector().mapFileSystem(types.FileSystemDescription{
		FileSystemId: aws.String("fs-xyz"),
	}, "us-east-1")
	if res.Name != "fs-xyz" {
		t.Errorf("Name = %q, want id fallback", res.Name)
	}
}
