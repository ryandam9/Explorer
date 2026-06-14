// Package efs collects EFS file systems. A typed collector is needed because
// the Resource Groups Tagging API only returns tagged resources; an untagged
// file system is invisible to the broad discovery sweep.
package efs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/efs/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "efs" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := efs.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var marker *string
	for {
		page, err := client.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{Marker: marker})
		if err != nil {
			return resources, fmt.Errorf("failed to describe EFS file systems: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.FileSystems))
		for _, fs := range page.FileSystems {
			batch = append(batch, c.mapFileSystem(fs, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextMarker == nil {
			break
		}
		marker = page.NextMarker
	}
	return resources, nil
}

func (c *Collector) mapFileSystem(fs types.FileSystemDescription, region string) model.Resource {
	id := aws.ToString(fs.FileSystemId)
	// EFS file systems have no name of their own; the console name is the value
	// of the Name tag, which DescribeFileSystems surfaces as Name.
	name := aws.ToString(fs.Name)
	if name == "" {
		name = id
	}
	return model.Resource{
		Service:   "efs",
		Type:      "fileSystem",
		Region:    region,
		ID:        id,
		Name:      name,
		ARN:       aws.ToString(fs.FileSystemArn),
		State:     string(fs.LifeCycleState),
		CreatedAt: fs.CreationTime,
		Summary: map[string]string{
			"mountTargets": fmt.Sprintf("%d", fs.NumberOfMountTargets),
		},
	}
}
