package lambdatui

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
)

func TestTabColumnsRegionColumn(t *testing.T) {
	single := tabColumns(tabFunctions, false)
	multi := tabColumns(tabFunctions, true)
	if len(multi) != len(single)+1 {
		t.Fatalf("multi-region should add one column: single=%d multi=%d", len(single), len(multi))
	}
	if multi[len(multi)-1].Title != "REGION" {
		t.Errorf("last multi column = %q, want REGION", multi[len(multi)-1].Title)
	}
}

func TestInventorySort(t *testing.T) {
	inv := Inventory{
		Functions: []Function{{Name: "b", Region: "us-east-1"}, {Name: "a", Region: "us-east-1"}},
		Layers:    []Layer{{Name: "z"}, {Name: "a"}},
	}
	inv.sort()
	if inv.Functions[0].Name != "a" || inv.Functions[1].Name != "b" {
		t.Errorf("functions not sorted: %+v", inv.Functions)
	}
	if inv.Layers[0].Name != "a" {
		t.Errorf("layers not sorted: %+v", inv.Layers)
	}
}

func TestMapFunction(t *testing.T) {
	fn := lambdatypes.FunctionConfiguration{
		FunctionName:     aws.String("orders"),
		FunctionArn:      aws.String("arn:fn:orders"),
		Runtime:          lambdatypes.RuntimePython312,
		MemorySize:       aws.Int32(256),
		Timeout:          aws.Int32(30),
		State:            lambdatypes.StateActive,
		DeadLetterConfig: &lambdatypes.DeadLetterConfig{TargetArn: aws.String("arn:aws:sqs:us-east-1:1:dl")},
		Environment:      &lambdatypes.EnvironmentResponse{Variables: map[string]string{"B": "2", "A": "1"}},
	}
	got := mapFunction("us-east-1", fn)
	if got.Name != "orders" || got.MemoryMB != 256 || got.TimeoutSec != 30 {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.DLQTargetArn == "" {
		t.Error("DLQ target should be captured")
	}
	if got.LogGroup != "/aws/lambda/orders" {
		t.Errorf("default log group = %q", got.LogGroup)
	}
	// Env-var keys are sorted; values must not be retained anywhere on the struct.
	if len(got.EnvVarKeys) != 2 || got.EnvVarKeys[0] != "A" || got.EnvVarKeys[1] != "B" {
		t.Errorf("env keys = %v", got.EnvVarKeys)
	}
}

func TestMapEventSource(t *testing.T) {
	m := lambdatypes.EventSourceMappingConfiguration{
		UUID:           aws.String("u1"),
		FunctionArn:    aws.String("arn:aws:lambda:us-east-1:123:function:orders"),
		EventSourceArn: aws.String("arn:aws:sqs:us-east-1:123:q"),
		State:          aws.String("Enabled"),
		BatchSize:      aws.Int32(10),
	}
	got := mapEventSource("us-east-1", m)
	if got.FunctionName != "orders" || got.SourceLabel != "sqs:q" || got.BatchSize != 10 {
		t.Errorf("unexpected mapping: %+v", got)
	}
}

func TestComputeFindings(t *testing.T) {
	mm := &m{
		regions: []string{"us-east-1"},
		inv: Inventory{Functions: []Function{
			{Name: "legacy", Region: "us-east-1", ARN: "arn:legacy", Runtime: "python3.7", DLQTargetArn: "arn:dl", State: "Active"},
		}},
	}
	fs := mm.computeFindings()
	var found bool
	for _, f := range fs {
		if f.ID == findings.CheckLambdaRuntimeDeprecated {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a deprecated-runtime finding, got %+v", fs)
	}
}
