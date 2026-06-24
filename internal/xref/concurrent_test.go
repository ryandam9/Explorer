package xref

import (
	"context"
	"fmt"
	"testing"
)

func TestBoundedEdges_OrderedAndComplete(t *testing.T) {
	items := make([]int, 100)
	for i := range items {
		items[i] = i
	}
	rec := &recorder{region: "us-east-1"}
	got := boundedEdges(context.Background(), items, 10, rec, func(_ context.Context, n int, _ *recorder) []Edge {
		return []Edge{{From: Reference{ID: fmt.Sprintf("r-%d", n)}, Target: fmt.Sprintf("t-%d", n)}}
	})
	if len(got) != len(items) {
		t.Fatalf("got %d edges, want %d", len(got), len(items))
	}
	// Results must be concatenated in input order (write-by-index).
	for i, e := range got {
		if e.Target != fmt.Sprintf("t-%d", i) {
			t.Fatalf("edge %d out of order: %+v", i, e)
		}
	}
}

func TestBoundedEdges_RecorderIsConcurrencySafe(t *testing.T) {
	items := make([]int, 200)
	rec := &recorder{region: "us-east-1"}
	// Every worker records the same error concurrently; the recorder must not
	// race and must collapse them to a single entry (§7 dedup).
	boundedEdges(context.Background(), items, 16, rec, func(_ context.Context, _ int, rec *recorder) []Edge {
		rec.record("kms", fmt.Errorf("AccessDenied: nope"))
		return nil
	})
	if len(rec.errs) != 1 {
		t.Errorf("expected identical errors collapsed to 1, got %d", len(rec.errs))
	}
}

// TestBoundedEdges_CollapsesThrottleStorm reproduces the CloudWatch Logs case:
// a large per-item sweep self-throttles, and each call fails with a "Rate
// exceeded" error carrying a distinct RequestID. Classify canonicalizes them,
// so the recorder must collapse the whole storm to a single line (§7).
func TestBoundedEdges_CollapsesThrottleStorm(t *testing.T) {
	items := make([]int, 50)
	rec := &recorder{region: "ap-southeast-2"}
	boundedEdges(context.Background(), items, 16, rec, func(_ context.Context, n int, rec *recorder) []Edge {
		rec.record("logs", fmt.Errorf("operation error CloudWatch Logs: DescribeSubscriptionFilters, "+
			"exceeded maximum number of attempts, 3, https response error StatusCode: 400, "+
			"RequestID: req-%d, api error ThrottlingException: Rate exceeded", n))
		return nil
	})
	if len(rec.errs) != 1 {
		t.Fatalf("throttle storm should collapse to 1 error, got %d", len(rec.errs))
	}
	if rec.errs[0].Code != "Throttling" {
		t.Errorf("expected code Throttling, got %q", rec.errs[0].Code)
	}
}

func TestBoundedEdges_Empty(t *testing.T) {
	rec := &recorder{}
	if got := boundedEdges(context.Background(), nil, 8, rec, func(context.Context, int, *recorder) []Edge {
		t.Fatal("fn must not run for empty input")
		return nil
	}); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}
