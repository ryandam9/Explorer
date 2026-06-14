package eventbridge

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
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

	if res.Type != "eventBus" {
		t.Errorf("Type = %q, want eventBus", res.Type)
	}
	if res.Name != "payments" {
		t.Errorf("Name = %q", res.Name)
	}
	if res.ARN != "arn:aws:events:us-east-1:123456789012:event-bus/payments" {
		t.Errorf("ARN = %q", res.ARN)
	}
}
