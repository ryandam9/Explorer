package eventbridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"

	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "eventbridge" {
		t.Errorf("Name() = %q, want eventbridge", c.Name())
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — EventBridge is regional")
	}
}

func TestMapRule(t *testing.T) {
	c := NewCollector()
	rule := types.Rule{
		Arn:                aws.String("arn:aws:events:us-east-1:123456789012:rule/nightly"),
		Name:               aws.String("nightly"),
		State:              types.RuleStateEnabled,
		EventBusName:       aws.String("default"),
		ScheduleExpression: aws.String("cron(0 3 * * ? *)"),
		Description:        aws.String("nightly batch"),
	}
	res := c.mapRule(rule, "us-east-1")

	if res.Service != "eventbridge" || res.Type != "rule" {
		t.Errorf("Service/Type = %q/%q", res.Service, res.Type)
	}
	if res.ID != "nightly" || res.Name != "nightly" {
		t.Errorf("ID/Name = %q/%q", res.ID, res.Name)
	}
	if res.ARN != "arn:aws:events:us-east-1:123456789012:rule/nightly" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.State != "ENABLED" {
		t.Errorf("State = %q, want ENABLED", res.State)
	}
	if res.Summary["eventBus"] != "default" {
		t.Errorf("Summary[eventBus] = %q", res.Summary["eventBus"])
	}
	if res.Summary["schedule"] != "cron(0 3 * * ? *)" {
		t.Errorf("Summary[schedule] = %q", res.Summary["schedule"])
	}
	if res.Summary["description"] != "nightly batch" {
		t.Errorf("Summary[description] = %q", res.Summary["description"])
	}
}

func TestMapRule_OptionalFieldsAbsent(t *testing.T) {
	res := NewCollector().mapRule(types.Rule{
		Arn:          aws.String("arn:aws:events:us-east-1:1:rule/r"),
		Name:         aws.String("r"),
		State:        types.RuleStateDisabled,
		EventBusName: aws.String("default"),
	}, "us-east-1")

	if _, ok := res.Summary["schedule"]; ok {
		t.Error("schedule should be absent for an event-pattern rule")
	}
	if _, ok := res.Summary["description"]; ok {
		t.Error("description should be absent when empty")
	}
}

func TestMapBus(t *testing.T) {
	res := NewCollector().mapBus(types.EventBus{
		Name: aws.String("payments"),
		Arn:  aws.String("arn:aws:events:us-east-1:123456789012:event-bus/payments"),
	}, "us-east-1")

	if res.Type != "event-bus" {
		t.Errorf("Type = %q, want event-bus", res.Type)
	}
	if res.Name != "payments" {
		t.Errorf("Name = %q", res.Name)
	}
	if res.ARN != "arn:aws:events:us-east-1:123456789012:event-bus/payments" {
		t.Errorf("ARN = %q", res.ARN)
	}
}

// fakeEB implements eventBridgeAPI with configurable buses and per-bus rule
// results/errors (keyed by bus name), single-page.
type fakeEB struct {
	buses    []types.EventBus
	rules    map[string][]types.Rule
	rulesErr map[string]error
}

func (f fakeEB) ListEventBuses(context.Context, *eventbridge.ListEventBusesInput, ...func(*eventbridge.Options)) (*eventbridge.ListEventBusesOutput, error) {
	return &eventbridge.ListEventBusesOutput{EventBuses: f.buses}, nil
}

func (f fakeEB) ListRules(_ context.Context, in *eventbridge.ListRulesInput, _ ...func(*eventbridge.Options)) (*eventbridge.ListRulesOutput, error) {
	name := aws.ToString(in.EventBusName)
	if err := f.rulesErr[name]; err != nil {
		return nil, err
	}
	return &eventbridge.ListRulesOutput{Rules: f.rules[name]}, nil
}

func TestCollect_OneBusRuleFailureKeepsOtherBuses(t *testing.T) {
	c := NewCollector()
	api := fakeEB{
		buses: []types.EventBus{
			{Name: aws.String("default"), Arn: aws.String("arn:default")},
			{Name: aws.String("busA"), Arn: aws.String("arn:busA")},
			{Name: aws.String("busB"), Arn: aws.String("arn:busB")},
		},
		rules: map[string][]types.Rule{
			"busB": {{Name: aws.String("ruleB"), Arn: aws.String("arn:ruleB")}},
		},
		rulesErr: map[string]error{"busA": errors.New("AccessDenied: ListRules")},
	}

	resources, err := c.collect(context.Background(), api, services.CollectInput{Region: "us-east-1"})
	if err == nil || !strings.Contains(err.Error(), "busA") {
		t.Fatalf("expected a joined error naming busA, got: %v", err)
	}

	var gotRuleB, gotBusB bool
	for _, r := range resources {
		if r.Type == "rule" && r.Name == "ruleB" {
			gotRuleB = true
		}
		if r.Type == "event-bus" && r.Name == "busB" {
			gotBusB = true
		}
		if r.Type == "event-bus" && r.Name == "default" {
			t.Error("default bus should not be emitted as a resource")
		}
	}
	if !gotRuleB {
		t.Error("busB's rule should be collected despite busA's failure")
	}
	if !gotBusB {
		t.Error("busB (custom bus) should be emitted as a resource")
	}
}
