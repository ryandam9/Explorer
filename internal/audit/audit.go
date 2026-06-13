// Package audit collects the AWS data the account-wide findings linters
// reason over and runs them across regions. Collection is best-effort, in
// the same spirit as the scan engine: a denied or throttled call degrades
// the affected checks (reported as collection errors) and never aborts the
// audit. All analysis logic lives in internal/findings; this package only
// talks to AWS and maps SDK types into snapshot structs.
package audit

import (
	"context"
	"log/slog"
	"strings"
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

// Stream runs the selected finding categories ("cost", "security") over
// every region, sending one CostChunk per region as it finishes and closing
// ch when all are done. baseCfg supplies credentials; the region is
// overridden per scan. perCallTimeout bounds each service-family collection
// within a region (0 = no timeout). Chunk findings are sorted within the
// region; consumers aggregating regions re-sort with findings.Sort.
//
// S3 is account-global, so its security sweep runs in the first region only
// and lands in that region's chunk.
func Stream(ctx context.Context, baseCfg aws.Config, regions []string, categories []string, maxConcurrency int, perCallTimeout time.Duration, ch chan<- CostChunk) {
	defer close(ch)
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}
	wantCost, wantSecurity, wantIAM, wantMessaging := false, false, false, false
	for _, c := range categories {
		switch c {
		case "cost":
			wantCost = true
		case "security":
			wantSecurity = true
		case "iam":
			wantIAM = true
		case "messaging":
			wantMessaging = true
		}
	}

	slog.Info("Starting audit", "regions", len(regions), "categories", strings.Join(categories, ","))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		region := region
		// The first region also carries the account-global sweeps (S3, IAM).
		s3Region := i == 0
		g.Go(func() error {
			slog.Info("Auditing region", "region", region)
			var fs []findings.Finding
			var errs []model.ExploreError
			if wantCost {
				snap, e := collectCostRegion(gctx, baseCfg, region, perCallTimeout)
				fs = append(fs, findings.AnalyzeCost(snap)...)
				errs = append(errs, e...)
			}
			if wantSecurity {
				snap, e := collectSecurityRegion(gctx, baseCfg, region, s3Region, perCallTimeout)
				fs = append(fs, findings.AnalyzeSecurity(snap)...)
				errs = append(errs, e...)
			}
			if wantMessaging {
				snap, e := collectMessagingRegion(gctx, baseCfg, region, perCallTimeout)
				fs = append(fs, findings.AnalyzeMessaging(snap)...)
				errs = append(errs, e...)
			}
			// IAM is account-global: collected once, in the first region's
			// pass, with findings labeled Region "global".
			if wantIAM && s3Region {
				snap, e := collectIAMAccount(gctx, baseCfg, perCallTimeout)
				fs = append(fs, findings.AnalyzeIAM(snap)...)
				errs = append(errs, e...)
			}
			findings.Sort(fs)
			slog.Info("Audited region", "region", region, "findings", len(fs), "errors", len(errs))
			select {
			case ch <- CostChunk{Region: region, Findings: fs, Errors: errs}:
			case <-gctx.Done():
			}
			return nil
		})
	}
	_ = g.Wait()
}

// Run executes the selected categories over every region and returns the
// merged, sorted findings plus any collection errors.
func Run(ctx context.Context, baseCfg aws.Config, regions []string, categories []string, maxConcurrency int, perCallTimeout time.Duration) ([]findings.Finding, []model.ExploreError) {
	ch := make(chan CostChunk, len(regions)+1)
	go Stream(ctx, baseCfg, regions, categories, maxConcurrency, perCallTimeout, ch)

	var fs []findings.Finding
	var errs []model.ExploreError
	for chunk := range ch {
		fs = append(fs, chunk.Findings...)
		errs = append(errs, chunk.Errors...)
	}
	findings.Sort(fs)
	return fs, errs
}

// StreamCost runs the cost/waste linter (AXE-004) only. Kept as the
// single-category convenience wrapper around Stream.
func StreamCost(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration, ch chan<- CostChunk) {
	Stream(ctx, baseCfg, regions, []string{"cost"}, maxConcurrency, perCallTimeout, ch)
}

// Cost runs the cost/waste linter only, returning merged sorted findings.
func Cost(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]findings.Finding, []model.ExploreError) {
	return Run(ctx, baseCfg, regions, []string{"cost"}, maxConcurrency, perCallTimeout)
}
