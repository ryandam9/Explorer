package iam

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// Collector implements the services.Collector interface for IAM.
type Collector struct{}

// NewCollector returns a new IAM Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "iam"
}

func (c *Collector) IsGlobal() bool {
	return true
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := iam.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	// 1. Collect Roles
	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list IAM roles: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Roles))
		for _, role := range page.Roles {
			batch = append(batch, c.mapRole(role, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapRole(role types.Role, detail services.DetailLevel) model.Resource {
	id := aws.ToString(role.RoleId)
	name := aws.ToString(role.RoleName)
	arn := aws.ToString(role.Arn)

	res := model.Resource{
		Service: "iam",
		Type:    "role",
		Region:  "global",
		ID:      id,
		Name:    name,
		ARN:     arn,
		Summary: map[string]string{
			"path": aws.ToString(role.Path),
		},
	}

	if role.CreateDate != nil {
		res.CreatedAt = role.CreateDate
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"assumeRolePolicyDocument": aws.ToString(role.AssumeRolePolicyDocument),
			"description":              aws.ToString(role.Description),
			"maxSessionDuration":       aws.ToInt32(role.MaxSessionDuration),
		}
	}

	return res
}
