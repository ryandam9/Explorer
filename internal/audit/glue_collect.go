package audit

import (
	"context"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	awsglue "github.com/aws/aws-sdk-go-v2/service/glue"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxGlueJobs        = 200
	maxGlueCrawlers    = 200
	maxGlueConnections = 200
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
	collectGlueConnections(ctx, cfg, &snap, rec, perCallTimeout)
	collectGlueNetworkRefs(ctx, cfg, &snap, rec, perCallTimeout)

	return snap, rec.errs
}

func collectGlueConnections(ctx context.Context, cfg aws.Config, snap *findings.GlueSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsglue.NewFromConfig(cfg)

	var token *string
	for {
		page, err := client.GetConnections(ctx, &awsglue.GetConnectionsInput{NextToken: token})
		if err != nil {
			rec.record("glue", err)
			break
		}
		for _, c := range page.ConnectionList {
			gc := findings.GlueConnection{
				Name: aws.ToString(c.Name),
				ARN:  glueARN(cfg.Region, "connection/"+aws.ToString(c.Name)),
			}
			if p := c.PhysicalConnectionRequirements; p != nil {
				gc.SubnetID = aws.ToString(p.SubnetId)
				gc.SecurityGroupIDs = p.SecurityGroupIdList
			}
			snap.Connections = append(snap.Connections, gc)
			if len(snap.Connections) >= maxGlueConnections {
				rec.recordTruncation("glue", "connections", maxGlueConnections)
				break
			}
		}
		if page.NextToken == nil || len(snap.Connections) >= maxGlueConnections {
			break
		}
		token = page.NextToken
	}
}

// collectGlueNetworkRefs inventories the region's subnets and security groups so
// the connection check can tell a deleted reference from a live one. It runs
// only when a connection actually has VPC requirements, and sets
// NetworkRefsKnown only when both describes succeed — a partial inventory would
// flag healthy references as missing, so on any error the check stays silent.
func collectGlueNetworkRefs(ctx context.Context, cfg aws.Config, snap *findings.GlueSnapshot, rec *errRecorder, timeout time.Duration) {
	need := false
	for _, c := range snap.Connections {
		if c.SubnetID != "" || len(c.SecurityGroupIDs) > 0 {
			need = true
			break
		}
	}
	if !need {
		return
	}

	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsec2.NewFromConfig(cfg)

	subnets := map[string]bool{}
	sp := awsec2.NewDescribeSubnetsPaginator(client, &awsec2.DescribeSubnetsInput{})
	for sp.HasMorePages() {
		page, err := sp.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			return
		}
		for _, s := range page.Subnets {
			subnets[aws.ToString(s.SubnetId)] = true
		}
	}

	sgs := map[string]bool{}
	gp := awsec2.NewDescribeSecurityGroupsPaginator(client, &awsec2.DescribeSecurityGroupsInput{})
	for gp.HasMorePages() {
		page, err := gp.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			return
		}
		for _, g := range page.SecurityGroups {
			sgs[aws.ToString(g.GroupId)] = true
		}
	}

	snap.ExistingSubnets = subnets
	snap.ExistingSGs = sgs
	snap.NetworkRefsKnown = true
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
