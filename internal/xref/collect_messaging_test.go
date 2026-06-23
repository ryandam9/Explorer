package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

func TestSNSSubscriptionEdges(t *testing.T) {
	subs := []snstypes.Subscription{
		{TopicArn: aws.String("arn:aws:sns:us-east-1:111:orders"), Protocol: aws.String("sqs"), Endpoint: aws.String("arn:aws:sqs:us-east-1:111:orders-q")},
		{TopicArn: aws.String("arn:aws:sns:us-east-1:111:orders"), Protocol: aws.String("lambda"), Endpoint: aws.String("arn:aws:lambda:us-east-1:111:function:notify")},
		{TopicArn: aws.String("arn:aws:sns:us-east-1:111:orders"), Protocol: aws.String("email"), Endpoint: aws.String("ops@example.com")},
		{TopicArn: aws.String("arn:aws:sns:us-east-1:111:orders"), Endpoint: aws.String("")}, // skipped
	}
	edges := snsSubscriptionEdges(subs, "us-east-1")
	if len(edges) != 3 {
		t.Fatalf("want 3 edges, got %d: %+v", len(edges), edges)
	}
	for _, e := range edges {
		if e.From.Service != "sns" || e.From.ID != "arn:aws:sns:us-east-1:111:orders" {
			t.Errorf("from = %+v", e.From)
		}
	}
	// Protocol shows in the via label.
	if edges[0].From.Via != "SNS subscription (sqs)" || edges[0].Target != "arn:aws:sqs:us-east-1:111:orders-q" {
		t.Errorf("edge0 = %+v", edges[0])
	}
}

func TestSNSSubscription_ResolvesBothDirections(t *testing.T) {
	edges := snsSubscriptionEdges([]snstypes.Subscription{
		{TopicArn: aws.String("arn:aws:sns:us-east-1:111:orders"), Protocol: aws.String("sqs"), Endpoint: aws.String("arn:aws:sqs:us-east-1:111:orders-q")},
	}, "us-east-1")
	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)

	topic := Related("arn:aws:sns:us-east-1:111:orders", fwd, rev, 1, false)
	if len(topic.Uses) != 1 || topic.Uses[0].Service != "sqs" {
		t.Fatalf("topic.Uses = %+v", topic.Uses)
	}
	queue := Related("arn:aws:sqs:us-east-1:111:orders-q", fwd, rev, 1, false)
	if len(queue.UsedBy) != 1 || queue.UsedBy[0].Service != "sns" {
		t.Fatalf("queue.UsedBy = %+v", queue.UsedBy)
	}
}

func TestSQSRedriveTarget(t *testing.T) {
	cases := map[string]string{
		`{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:111:dlq","maxReceiveCount":5}`: "arn:aws:sqs:us-east-1:111:dlq",
		`{"maxReceiveCount":5}`: "", // no DLQ arn
		``:                      "", // unset
		`not json`:              "", // unparseable
	}
	for policy, want := range cases {
		if got := sqsRedriveTarget(policy); got != want {
			t.Errorf("sqsRedriveTarget(%q) = %q, want %q", policy, got, want)
		}
	}
}

func TestEventBridgeTargetEdges(t *testing.T) {
	ruleRef := Reference{Service: "events", Type: "rule", Region: "us-east-1",
		ID: "arn:aws:events:us-east-1:111:rule/cron", Name: "cron"}
	targets := []ebtypes.Target{
		{Id: aws.String("1"), Arn: aws.String("arn:aws:lambda:us-east-1:111:function:job")},
		{Id: aws.String("2"), Arn: aws.String("arn:aws:sqs:us-east-1:111:q"),
			DeadLetterConfig: &ebtypes.DeadLetterConfig{Arn: aws.String("arn:aws:sqs:us-east-1:111:dlq")}},
		{Id: aws.String("3")}, // no arn → skipped (but no DLQ either)
	}
	edges := eventBridgeTargetEdges(ruleRef, targets)
	got := viaTargets(edges)
	if got["EventBridge target"] == "" {
		t.Errorf("missing target edge: %+v", edges)
	}
	if got["EventBridge dead-letter"] != "arn:aws:sqs:us-east-1:111:dlq" {
		t.Errorf("missing dead-letter edge: %+v", edges)
	}
	// 2 targets with arn + 1 dlq = 3 edges.
	if len(edges) != 3 {
		t.Errorf("want 3 edges, got %d: %+v", len(edges), edges)
	}
}

func TestSFNDefinitionARNs(t *testing.T) {
	self := "arn:aws:states:us-east-1:111:stateMachine:flow"
	def := `{
		"StartAt": "Invoke",
		"States": {
			"Invoke": {"Type":"Task","Resource":"arn:aws:lambda:us-east-1:111:function:step1","Next":"Notify"},
			"Notify": {"Type":"Task","Resource":"arn:aws:states:::sns:publish","Parameters":{"TopicArn":"arn:aws:sns:us-east-1:111:done"}},
			"Self":  {"Type":"Task","Resource":"arn:aws:states:us-east-1:111:stateMachine:flow"}
		}
	}`
	got := sfnDefinitionARNs(def, self)

	want := map[string]bool{
		"arn:aws:lambda:us-east-1:111:function:step1": true,
		"arn:aws:states:::sns:publish":                true, // service-integration ARN
		"arn:aws:sns:us-east-1:111:done":              true,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d arns, got %d: %v", len(want), len(got), got)
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("unexpected arn %q", a)
		}
		if a == self {
			t.Errorf("self ARN should be excluded")
		}
	}
}

func TestCheckedTypes_MessagingRegistered(t *testing.T) {
	kms := CheckedTypes(KindKMSKey)
	role := CheckedTypes(KindIAMRole)
	hasKMS := contains(kms, "SNS topic encryption") && contains(kms, "Kinesis stream encryption")
	if !hasKMS {
		t.Errorf("KMS CheckedTypes missing SNS/Kinesis: %v", kms)
	}
	if !contains(role, "Step Functions execution roles") {
		t.Errorf("IAM CheckedTypes missing Step Functions: %v", role)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
