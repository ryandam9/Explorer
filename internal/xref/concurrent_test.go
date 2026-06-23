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

func TestBoundedEdges_Empty(t *testing.T) {
	rec := &recorder{}
	if got := boundedEdges(context.Background(), nil, 8, rec, func(context.Context, int, *recorder) []Edge {
		t.Fatal("fn must not run for empty input")
		return nil
	}); got != nil {
		t.Errorf("empty input should return nil, got %v", got)
	}
}
