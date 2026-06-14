// Package ecr collects ECR repositories. A typed collector is needed because
// the Resource Groups Tagging API only returns tagged resources; an untagged
// repository is invisible to the broad discovery sweep.
package ecr

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "ecr" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := ecr.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to describe ECR repositories: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Repositories))
		for _, r := range page.Repositories {
			batch = append(batch, c.mapRepository(r, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapRepository(r types.Repository, region string) model.Resource {
	name := aws.ToString(r.RepositoryName)
	res := model.Resource{
		Service:   "ecr",
		Type:      "repository",
		Region:    region,
		ID:        name,
		Name:      name,
		ARN:       aws.ToString(r.RepositoryArn),
		CreatedAt: r.CreatedAt,
		Summary:   map[string]string{},
	}
	if uri := aws.ToString(r.RepositoryUri); uri != "" {
		res.Summary["uri"] = uri
	}
	return res
}
