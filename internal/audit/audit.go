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

// Cost runs the cost/waste linter (AXE-004) over every region and returns the
// merged, sorted findings plus any collection errors. baseCfg supplies
// credentials; the region is overridden per scan. perCallTimeout bounds each
// service-family collection within a region (0 = no timeout).
func Cost(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]findings.Finding, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	type regionResult struct {
		findings []findings.Finding
		errs     []model.ExploreError
	}
	results := make([]regionResult, len(regions))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			snap, errs := collectCostRegion(gctx, baseCfg, region, perCallTimeout)
			results[i] = regionResult{
				findings: findings.AnalyzeCost(snap),
				errs:     errs,
			}
			return nil
		})
	}
	_ = g.Wait()

	var fs []findings.Finding
	var errs []model.ExploreError
	for _, r := range results {
		fs = append(fs, r.findings...)
		errs = append(errs, r.errs...)
	}
	findings.Sort(fs)
	return fs, errs
}
