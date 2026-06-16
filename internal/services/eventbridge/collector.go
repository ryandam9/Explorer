// Package eventbridge collects EventBridge rules and custom event buses. A
// typed collector is needed because the Resource Groups Tagging API only
// returns resources that are tagged; an untagged rule or bus is invisible to
// the broad discovery sweep. Rules are listed per bus so rules on custom event
// buses are included, not just the default bus.
package eventbridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// eventBridgeAPI is the subset of the EventBridge client used by the collector,
// extracted so per-bus failure handling can be unit-tested with a fake.
type eventBridgeAPI interface {
	ListEventBuses(context.Context, *eventbridge.ListEventBusesInput, ...func(*eventbridge.Options)) (*eventbridge.ListEventBusesOutput, error)
	ListRules(context.Context, *eventbridge.ListRulesInput, ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error)
}

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
	return c.collect(ctx, eventbridge.NewFromConfig(input.AWSConfig), input)
}

// collect lists each event bus's rules independently: if one bus's ListRules
// is denied or fails, that bus is recorded as a partial error and the remaining
// buses are still collected, rather than aborting the whole region.
func (c *Collector) collect(ctx context.Context, client eventBridgeAPI, input services.CollectInput) ([]model.Resource, error) {
	buses, err := c.listBuses(ctx, client)
	if err != nil {
		// listBuses returns whatever pages it gathered before failing, so keep
		// going with those rather than discarding them.
		err = fmt.Errorf("failed to list EventBridge event buses: %w", err)
	}

	var resources []model.Resource
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
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
			errs = append(errs, fmt.Errorf("failed to list EventBridge rules on bus %q: %w", name, err))
		}
		batch := make([]model.Resource, 0, len(rules))
		for _, rule := range rules {
			batch = append(batch, c.mapRule(rule, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
	}
	return resources, errors.Join(errs...)
}

func (c *Collector) listBuses(ctx context.Context, client eventBridgeAPI) ([]types.EventBus, error) {
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

func (c *Collector) listRules(ctx context.Context, client eventBridgeAPI, busName string) ([]types.Rule, error) {
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
		Type:    "event-bus",
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
