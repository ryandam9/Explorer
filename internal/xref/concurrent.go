package xref

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// boundedEdges runs fn over items with bounded concurrency and concatenates the
// per-item edges in input order (write-by-index, so the result is
// deterministic and race-free). It is the per-item-sweep counterpart to the
// per-region fan-out: many collectors otherwise make N Describe/Get calls
// sequentially under one region deadline, so a large account serializes (and,
// when the deadline expires, every remaining call fails) — CLAUDE.md §7.
//
// fn records its own errors into the shared recorder, which is concurrency-safe.
// Workers run under ctx (which already carries the region deadline), so a slow
// item can't outrun the region budget.
func boundedEdges[T any](ctx context.Context, items []T, maxConcurrency int, rec *recorder, fn func(context.Context, T, *recorder) []Edge) []Edge {
	if len(items) == 0 {
		return nil
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	edgesByIdx := make([][]Edge, len(items))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, it := range items {
		i, it := i, it
		g.Go(func() error {
			edgesByIdx[i] = fn(gctx, it, rec)
			return nil // best-effort: a failed item is recorded, never fatal
		})
	}
	_ = g.Wait()

	var edges []Edge
	for i := range items {
		edges = append(edges, edgesByIdx[i]...)
	}
	return edges
}
