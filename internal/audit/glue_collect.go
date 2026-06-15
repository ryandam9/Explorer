package audit

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxGlueJobs        = 200
	maxGlueCrawlers    = 200
	glueRunWindow      = 20 // recent runs fetched per job for the streak/cost checks
	glueRunConcurrency = 8
)

// collectGlueRegion gathers the Glue health/cost snapshot for one region. Same
// best-effort contract as the other collectors: a denied call degrades the
// affected checks (recorded as a collection error) and never aborts the audit.
func collectGlueRegion(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.GlueSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.GlueSnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	collectGlueJobs(ctx, cfg, &snap, rec, perCallTimeout)
	collectGlueCrawlers(ctx, cfg, &snap, rec, perCallTimeout)

	return snap, rec.errs
}

func collectGlueJobs(ctx context.Context, cfg aws.Config, snap *findings.GlueSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsglue.NewFromConfig(cfg)

	var token *string
	for {
		page, err := client.GetJobs(ctx, &awsglue.GetJobsInput{NextToken: token})
		if err != nil {
			rec.record("glue", err)
			break
		}
		for _, j := range page.Jobs {
			snap.Jobs = append(snap.Jobs, findings.GlueJob{
				Name:              aws.ToString(j.Name),
				ARN:               glueARN(cfg.Region, "job/"+aws.ToString(j.Name)),
				HasSecurityConfig: aws.ToString(j.SecurityConfiguration) != "",
				NumberOfWorkers:   aws.ToInt32(j.NumberOfWorkers),
			})
			if len(snap.Jobs) >= maxGlueJobs {
				rec.recordTruncation("glue", "jobs", maxGlueJobs)
				break
			}
		}
		if page.NextToken == nil || len(snap.Jobs) >= maxGlueJobs {
			break
		}
		token = page.NextToken
	}

	// Fetch each job's recent run history concurrently. A per-job failure
	// leaves RunsKnown=false so the run-based checks stay silent for it.
	var g errgroup.Group
	g.SetLimit(glueRunConcurrency)
	var mu sync.Mutex
	for i := range snap.Jobs {
		i := i
		g.Go(func() error {
			out, err := client.GetJobRuns(ctx, &awsglue.GetJobRunsInput{
				JobName: aws.String(snap.Jobs[i].Name), MaxResults: aws.Int32(glueRunWindow),
			})
			if err != nil {
				mu.Lock()
				rec.record("glue", err)
				mu.Unlock()
				return nil
			}
			runs := make([]findings.GlueRun, 0, len(out.JobRuns))
			for _, r := range out.JobRuns {
				gr := findings.GlueRun{
					State:      string(r.JobRunState),
					ExecSecs:   r.ExecutionTime,
					DPUSeconds: aws.ToFloat64(r.DPUSeconds),
				}
				if r.StartedOn != nil {
					gr.Started = *r.StartedOn
				}
				runs = append(runs, gr)
			}
			snap.Jobs[i].RunsKnown = true
			snap.Jobs[i].Runs = runs
			return nil
		})
	}
	_ = g.Wait()
}

func collectGlueCrawlers(ctx context.Context, cfg aws.Config, snap *findings.GlueSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsglue.NewFromConfig(cfg)

	var token *string
	for {
		page, err := client.GetCrawlers(ctx, &awsglue.GetCrawlersInput{NextToken: token})
		if err != nil {
			rec.record("glue", err)
			break
		}
		for _, cr := range page.Crawlers {
			gc := findings.GlueCrawler{
				Name:           aws.ToString(cr.Name),
				ARN:            glueARN(cfg.Region, "crawler/"+aws.ToString(cr.Name)),
				State:          string(cr.State),
				RunningElapsed: time.Duration(cr.CrawlElapsedTime) * time.Millisecond,
			}
			if cr.LastCrawl != nil {
				gc.LastCrawlStatus = string(cr.LastCrawl.Status)
			}
			snap.Crawlers = append(snap.Crawlers, gc)
			if len(snap.Crawlers) >= maxGlueCrawlers {
				rec.recordTruncation("glue", "crawlers", maxGlueCrawlers)
				break
			}
		}
		if page.NextToken == nil || len(snap.Crawlers) >= maxGlueCrawlers {
			break
		}
		token = page.NextToken
	}
}

// glueARN synthesizes a Glue ARN (account omitted — the audit findings key on
// region+name, and the console ARN-search fallback resolves a partial ARN).
func glueARN(region, resource string) string {
	return "arn:aws:glue:" + region + "::" + resource
}
