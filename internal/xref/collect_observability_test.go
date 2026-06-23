package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestMetricAlarmEdges(t *testing.T) {
	a := cwtypes.MetricAlarm{
		AlarmName:               aws.String("cpu-high"),
		AlarmArn:                aws.String("arn:aws:cloudwatch:us-east-1:111:alarm:cpu-high"),
		AlarmActions:            []string{"arn:aws:sns:us-east-1:111:ops"},
		OKActions:               []string{"arn:aws:sns:us-east-1:111:ops"},
		InsufficientDataActions: []string{"not-an-arn"}, // skipped
	}
	edges := metricAlarmEdges(a, "us-east-1")
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.From.Type != "alarm" || e.From.Via != "alarm action" || e.Target != "arn:aws:sns:us-east-1:111:ops" {
			t.Errorf("edge = %+v", e)
		}
	}
}

func TestMetricAlarmEdge_ResolvesBothDirections(t *testing.T) {
	a := cwtypes.MetricAlarm{
		AlarmName:    aws.String("cpu-high"),
		AlarmArn:     aws.String("arn:aws:cloudwatch:us-east-1:111:alarm:cpu-high"),
		AlarmActions: []string{"arn:aws:sns:us-east-1:111:ops"},
	}
	edges := metricAlarmEdges(a, "us-east-1")
	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)
	topic := Related("arn:aws:sns:us-east-1:111:ops", fwd, rev, 1, false)
	if len(topic.UsedBy) != 1 || topic.UsedBy[0].Service != "cloudwatch" {
		t.Fatalf("topic.UsedBy = %+v", topic.UsedBy)
	}
}

func TestSubscriptionFilterEdges(t *testing.T) {
	ref := logGroupRef(cwltypes.LogGroup{LogGroupName: aws.String("/aws/lambda/checkout")}, "us-east-1")
	if ref.ID != "/aws/lambda/checkout" {
		t.Fatalf("log group ref ID = %q, want the name", ref.ID)
	}
	filters := []cwltypes.SubscriptionFilter{
		{FilterName: aws.String("to-kinesis"), DestinationArn: aws.String("arn:aws:kinesis:us-east-1:111:stream/logs")},
		{FilterName: aws.String("empty")}, // no destination → skipped
	}
	edges := subscriptionFilterEdges(ref, filters)
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	if edges[0].From.Via != "subscription filter" || edges[0].Target != "arn:aws:kinesis:us-east-1:111:stream/logs" {
		t.Errorf("edge = %+v", edges[0])
	}
}

// Log groups are keyed by name so they unify with the derived Lambda→log-group
// edge: querying the log group name resolves both the owning Lambda and the
// subscription destination.
func TestLogGroup_UnifiesWithLambdaEdge(t *testing.T) {
	lgName := "/aws/lambda/checkout"
	ref := logGroupRef(cwltypes.LogGroup{LogGroupName: aws.String(lgName)}, "us-east-1")
	edges := subscriptionFilterEdges(ref, []cwltypes.SubscriptionFilter{
		{DestinationArn: aws.String("arn:aws:kinesis:us-east-1:111:stream/logs")},
	})
	// Lambda → its log group (by convention), from the compute extractor.
	edges = append(edges, Edge{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1",
		ID: "arn:aws:lambda:us-east-1:111:function:checkout", Name: "checkout", Via: "CloudWatch log group (by convention)"}, Target: lgName})

	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)
	res := Related(lgName, fwd, rev, 1, false)
	if len(res.UsedBy) != 1 || res.UsedBy[0].Service != "lambda" {
		t.Errorf("log group UsedBy should include the Lambda: %+v", res.UsedBy)
	}
	if len(res.Uses) != 1 || res.Uses[0].Service != "kinesis" {
		t.Errorf("log group Uses should include the subscription destination: %+v", res.Uses)
	}
}

func TestCheckedTypes_ObservabilityRegistered(t *testing.T) {
	if !contains(CheckedTypes(KindKMSKey), "CloudWatch log group encryption") {
		t.Errorf("KMS CheckedTypes missing CloudWatch log group encryption")
	}
}
