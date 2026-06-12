// Package audit collects the AWS data the account-wide findings linters
// reason over and runs them across regions. Collection is best-effort, in
// the same spirit as the scan engine: a denied or throttled call degrades
// the affected checks (reported as collection errors) and never aborts the
// audit. All analysis logic lives in internal/findings; this package only
// talks to AWS and maps SDK types into snapshot structs.
package audit

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// CostChunk is the result of auditing one region, emitted as soon as that
// region's scan completes so consumers (the TUI) can show findings while
// other regions are still being scanned.
type CostChunk struct {
	Region   string
	Findings []findings.Finding
	Errors   []model.ExploreError
}

// StreamCost runs the cost/waste linter (AXE-004) over every region, sending
// one CostChunk per region as it finishes and closing ch when all are done.
// baseCfg supplies credentials; the region is overridden per scan.
// perCallTimeout bounds each service-family collection within a region
// (0 = no timeout). Chunk findings are sorted within the region; consumers
// aggregating regions re-sort with findings.Sort.
func StreamCost(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration, ch chan<- CostChunk) {
	defer close(ch)
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for _, region := range regions {
		region := region
		g.Go(func() error {
			snap, errs := collectCostRegion(gctx, baseCfg, region, perCallTimeout)
			fs := findings.AnalyzeCost(snap)
			findings.Sort(fs)
			select {
			case ch <- CostChunk{Region: region, Findings: fs, Errors: errs}:
			case <-gctx.Done():
			}
			return nil
		})
	}
	_ = g.Wait()
}

// Cost runs the cost/waste linter over every region and returns the merged,
// sorted findings plus any collection errors.
func Cost(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]findings.Finding, []model.ExploreError) {
	ch := make(chan CostChunk, len(regions)+1)
	go StreamCost(ctx, baseCfg, regions, maxConcurrency, perCallTimeout, ch)

	var fs []findings.Finding
	var errs []model.ExploreError
	for chunk := range ch {
		fs = append(fs, chunk.Findings...)
		errs = append(errs, chunk.Errors...)
	}
	findings.Sort(fs)
	return fs, errs
}
