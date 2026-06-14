// Package stepfunctions collects Step Functions state machines. A typed
// collector is needed because the Resource Groups Tagging API only returns
// resources that are tagged; an untagged state machine is invisible to the
// broad discovery sweep.
package stepfunctions

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "stepfunctions"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := sfn.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.ListStateMachines(ctx, &sfn.ListStateMachinesInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to list Step Functions state machines: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.StateMachines))
		for _, sm := range page.StateMachines {
			batch = append(batch, c.mapStateMachine(sm, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)

		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapStateMachine(sm types.StateMachineListItem, region string) model.Resource {
	name := aws.ToString(sm.Name)
	return model.Resource{
		Service:   "stepfunctions",
		Type:      "stateMachine",
		Region:    region,
		ID:        name,
		Name:      name,
		ARN:       aws.ToString(sm.StateMachineArn),
		CreatedAt: sm.CreationDate,
		Summary: map[string]string{
			// STANDARD vs EXPRESS changes pricing and semantics, so surface it.
			"type": string(sm.Type),
		},
	}
}
