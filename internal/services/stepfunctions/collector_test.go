package stepfunctions

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "stepfunctions" {
		t.Errorf("Name() = %q, want stepfunctions", c.Name())
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — Step Functions is regional")
	}
}

func TestMapStateMachine(t *testing.T) {
	c := NewCollector()
	created := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	sm := types.StateMachineListItem{
		StateMachineArn: aws.String("arn:aws:states:us-east-1:123456789012:stateMachine:etl"),
		Name:            aws.String("etl"),
		Type:            types.StateMachineTypeStandard,
		CreationDate:    aws.Time(created),
	}
	res := c.mapStateMachine(sm, "us-east-1")

	if res.Service != "stepfunctions" || res.Type != "state-machine" {
		t.Errorf("Service/Type = %q/%q", res.Service, res.Type)
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q", res.Region)
	}
	if res.ID != "etl" || res.Name != "etl" {
		t.Errorf("ID/Name = %q/%q", res.ID, res.Name)
	}
	if res.ARN != "arn:aws:states:us-east-1:123456789012:stateMachine:etl" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
	if res.Summary["type"] != "STANDARD" {
		t.Errorf("Summary[type] = %q, want STANDARD", res.Summary["type"])
	}
}
