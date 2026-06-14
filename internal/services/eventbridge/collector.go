// Package eventbridge collects EventBridge rules and custom event buses. A
// typed collector is needed because the Resource Groups Tagging API only
// returns resources that are tagged; an untagged rule or bus is invisible to
// the broad discovery sweep. Rules are listed per bus so rules on custom event
// buses are included, not just the default bus.
package eventbridge

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "eventbridge"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := eventbridge.NewFromConfig(input.AWSConfig)

	buses, err := c.listBuses(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to list EventBridge event buses: %w", err)
	}

	var resources []model.Resource
	for _, bus := range buses {
		name := aws.ToString(bus.Name)
		// The default bus exists in every account/region; surfacing it as a
		// resource would be noise, so only custom buses are listed as resources.
		// Its rules are still collected below.
		if name != "default" {
			resources = input.EmitOrAppend(resources, []model.Resource{c.mapBus(bus, input.Region)})
		}

		rules, err := c.listRules(ctx, client, name)
		if err != nil {
			return resources, fmt.Errorf("failed to list EventBridge rules on bus %q: %w", name, err)
		}
		batch := make([]model.Resource, 0, len(rules))
		for _, rule := range rules {
			batch = append(batch, c.mapRule(rule, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
	}
	return resources, nil
}

func (c *Collector) listBuses(ctx context.Context, client *eventbridge.Client) ([]types.EventBus, error) {
	var out []types.EventBus
	var token *string
	for {
		page, err := client.ListEventBuses(ctx, &eventbridge.ListEventBusesInput{NextToken: token})
		if err != nil {
			return out, err
		}
		out = append(out, page.EventBuses...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) listRules(ctx context.Context, client *eventbridge.Client, busName string) ([]types.Rule, error) {
	var out []types.Rule
	var token *string
	for {
		page, err := client.ListRules(ctx, &eventbridge.ListRulesInput{
			EventBusName: aws.String(busName),
			NextToken:    token,
		})
		if err != nil {
			return out, err
		}
		out = append(out, page.Rules...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) mapBus(bus types.EventBus, region string) model.Resource {
	name := aws.ToString(bus.Name)
	return model.Resource{
		Service: "eventbridge",
		Type:    "eventBus",
		Region:  region,
		ID:      name,
		Name:    name,
		ARN:     aws.ToString(bus.Arn),
	}
}

func (c *Collector) mapRule(rule types.Rule, region string) model.Resource {
	name := aws.ToString(rule.Name)
	res := model.Resource{
		Service: "eventbridge",
		Type:    "rule",
		Region:  region,
		ID:      name,
		Name:    name,
		ARN:     aws.ToString(rule.Arn),
		State:   string(rule.State),
		Summary: map[string]string{"eventBus": aws.ToString(rule.EventBusName)},
	}
	if s := aws.ToString(rule.ScheduleExpression); s != "" {
		res.Summary["schedule"] = s
	}
	if d := aws.ToString(rule.Description); d != "" {
		res.Summary["description"] = d
	}
	return res
}
