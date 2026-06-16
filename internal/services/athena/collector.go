// Package athena collects Athena workgroups. A typed collector is needed
// because the Resource Groups Tagging API only returns tagged resources; an
// untagged workgroup is invisible to the broad discovery sweep.
package athena

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "athena" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := athena.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.ListWorkGroups(ctx, &athena.ListWorkGroupsInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to list Athena workgroups: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.WorkGroups))
		for _, wg := range page.WorkGroups {
			batch = append(batch, c.mapWorkGroup(wg, input.Region, input.AccountID))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapWorkGroup(wg types.WorkGroupSummary, region, account string) model.Resource {
	name := aws.ToString(wg.Name)
	res := model.Resource{
		Service: "athena",
		Type:    "work-group",
		Region:  region,
		ID:      name,
		Name:    name,
		// ListWorkGroups returns no ARN, so construct it to match the Tagging
		// API form so the two merge.
		ARN:       fmt.Sprintf("arn:aws:athena:%s:%s:workgroup/%s", region, account, name),
		State:     string(wg.State),
		CreatedAt: wg.CreationTime,
		Summary:   map[string]string{},
	}
	if d := aws.ToString(wg.Description); d != "" {
		res.Summary["description"] = d
	}
	return res
}
