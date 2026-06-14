// Package cloudformation collects CloudFormation stacks. A typed collector is
// needed because the Resource Groups Tagging API only returns tagged resources;
// an untagged stack is invisible to the broad discovery sweep.
package cloudformation

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "cloudformation" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := cloudformation.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to describe CloudFormation stacks: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Stacks))
		for _, s := range page.Stacks {
			batch = append(batch, c.mapStack(s, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapStack(s types.Stack, region string) model.Resource {
	name := aws.ToString(s.StackName)
	return model.Resource{
		Service: "cloudformation",
		Type:    "stack",
		Region:  region,
		ID:      name,
		Name:    name,
		// StackId is the stack's ARN.
		ARN:       aws.ToString(s.StackId),
		State:     string(s.StackStatus),
		CreatedAt: s.CreationTime,
	}
}
