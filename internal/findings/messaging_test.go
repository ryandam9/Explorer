package findings

import (
	"strings"
	"testing"
	"time"
)

func intp(n int) *int { return &n }

func msgSnap(queues []MsgQueue, topics []MsgTopic) MessagingSnapshot {
	return MessagingSnapshot{
		Region: "us-east-1", Now: time.Now(),
		Queues: queues, QueuesComplete: true, Topics: topics,
	}
}

func TestAnalyzeMessaging_CleanSnapshotNoFindings(t *testing.T) {
	snap := msgSnap(
		[]MsgQueue{
			{Name: "busy", ARN: "arn:q:busy", Depth: 10, NotVisible: 3, ReceiveActivityKnown: true, Receives: 500},
			{Name: "with-dlq", ARN: "arn:q:with-dlq", RedriveARN: "arn:q:dlq", ReceiveActivityKnown: true, Receives: 9},
			{Name: "dlq", ARN: "arn:q:dlq", Depth: 0},
		},
		[]MsgTopic{{Name: "alerts", SubscriptionsConfirmed: intp(2), SubscriptionsPending: intp(0)}},
	)
	if fs := AnalyzeMessaging(snap); len(fs) != 0 {
		t.Errorf("clean snapshot produced findings: %+v", fs)
	}
}

func TestAnalyzeMessaging_NoConsumers(t *testing.T) {
	snap := msgSnap([]MsgQueue{
		{Name: "abandoned", Depth: 42, NotVisible: 0, ReceiveActivityKnown: true, Receives: 0},
		{Name: "in-flight", Depth: 42, NotVisible: 5, ReceiveActivityKnown: true, Receives: 0},
		{Name: "metrics-unknown", Depth: 42, NotVisible: 0, ReceiveActivityKnown: false},
		{Name: "empty", Depth: 0, ReceiveActivityKnown: true, Receives: 0},
	}, nil)
	fs := AnalyzeMessaging(snap)
	if len(fs) != 1 || fs[0].ID != CheckQueueNoConsumers || fs[0].Resource != "abandoned" {
		t.Errorf("findings = %+v", fs)
	}
}

func TestAnalyzeMessaging_DanglingRedrive(t *testing.T) {
	queues := []MsgQueue{
		{Name: "orders", ARN: "arn:q:orders", RedriveARN: "arn:q:gone"},
		{Name: "ok", ARN: "arn:q:ok", RedriveARN: "arn:q:dlq"},
		{Name: "dlq", ARN: "arn:q:dlq"},
	}
	fs := AnalyzeMessaging(msgSnap(queues, nil))
	if len(fs) != 1 || fs[0].ID != CheckRedriveDangling || fs[0].Resource != "orders" {
		t.Fatalf("findings = %+v", fs)
	}

	// A partial queue listing must not produce dangling-redrive findings.
	partial := msgSnap(queues, nil)
	partial.QueuesComplete = false
	for _, f := range AnalyzeMessaging(partial) {
		if f.ID == CheckRedriveDangling {
			t.Error("dangling redrive flagged from an incomplete listing")
		}
	}
}

func TestAnalyzeMessaging_DLQWithMessages(t *testing.T) {
	snap := msgSnap([]MsgQueue{
		{Name: "orders", ARN: "arn:q:orders", RedriveARN: "arn:q:dlq", ReceiveActivityKnown: true, Receives: 10},
		{Name: "payments", ARN: "arn:q:payments", RedriveARN: "arn:q:dlq", ReceiveActivityKnown: true, Receives: 10},
		{Name: "dlq", ARN: "arn:q:dlq", Depth: 7},
	}, nil)
	fs := AnalyzeMessaging(snap)
	if len(fs) != 1 || fs[0].ID != CheckDLQNotEmpty || fs[0].Resource != "dlq" {
		t.Fatalf("findings = %+v", fs)
	}
	if !strings.Contains(fs[0].Detail, "orders") || !strings.Contains(fs[0].Detail, "payments") {
		t.Errorf("detail should name the source queues: %q", fs[0].Detail)
	}
}

func TestAnalyzeMessaging_Topics(t *testing.T) {
	snap := msgSnap(nil, []MsgTopic{
		{Name: "pending", SubscriptionsConfirmed: intp(1), SubscriptionsPending: intp(2)},
		{Name: "deaf", SubscriptionsConfirmed: intp(0), SubscriptionsPending: intp(0)},
		{Name: "unknown"}, // attributes call failed: silent
	})
	fs := AnalyzeMessaging(snap)
	got := ids(fs)
	if got[CheckSubPending] != 1 || got[CheckTopicNoSubs] != 1 || len(fs) != 2 {
		t.Errorf("findings = %+v", fs)
	}
}

func TestParseRedriveTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:1:dlq","maxReceiveCount":"5"}`, "arn:aws:sqs:us-east-1:1:dlq"},
		{"", ""},
		{"not json", ""},
		{`{"maxReceiveCount":"5"}`, ""},
	}
	for _, c := range cases {
		if got := ParseRedriveTarget(c.in); got != c.want {
			t.Errorf("ParseRedriveTarget(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
