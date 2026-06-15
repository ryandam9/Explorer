// Package gluetui is the interactive AWS Glue dashboard (AXE-026/AXE-027): a
// tabbed Bubble Tea TUI over jobs, crawlers, triggers, workflows, connections
// and databases, with a per-job run-history drill-down (state, duration,
// DPU-hours and an estimated cost).
package gluetui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// Job, Crawler, Trigger, Workflow, Connection and Database are the dashboard's
// view of each Glue resource — only the fields the tables and console links
// need, flattened from the SDK types and annotated with their region.
type Job struct {
	Name           string
	Region         string
	ARN            string
	LastRunState   string
	LastRunStarted time.Time
	LastRunSeconds int32
	Worker         string
	GlueVersion    string
}

type Crawler struct {
	Name            string
	Region          string
	ARN             string
	State           string
	LastCrawlStatus string
	Database        string
	Schedule        string
}

type Trigger struct {
	Name     string
	Region   string
	ARN      string
	Type     string
	State    string
	Schedule string
	Workflow string
}

type Workflow struct {
	Name   string
	Region string
	ARN    string
}

type Connection struct {
	Name   string
	Region string
	ARN    string
	Type   string
	Status string
}

type Database struct {
	Name        string
	Region      string
	ARN         string
	Description string
}

// JobRun is one execution of a job, flattened for the run-history view.
type JobRun struct {
	ID         string
	State      string
	Error      string
	Started    time.Time
	Completed  time.Time
	ExecSecs   int32
	DPUSeconds float64
	Worker     string
	Attempt    int32
}

// JobDef is a job's configuration, flattened for the definition detail panel
// (AXE-029). DefaultArguments has secret-looking values redacted.
type JobDef struct {
	Name             string
	Region           string
	Role             string
	GlueVersion      string
	ExecutionClass   string
	Worker           string
	TimeoutMinutes   int32
	MaxRetries       int32
	Script           string
	SecurityConfig   string
	Connections      []string
	BookmarkEnabled  bool
	DefaultArguments map[string]string
}

// Inventory is the full set of Glue resources gathered across regions.
type Inventory struct {
	Jobs        []Job
	Crawlers    []Crawler
	Triggers    []Trigger
	Workflows   []Workflow
	Connections []Connection
	Databases   []Database
}

// Client holds one Glue client per region plus the account ID (needed to
// synthesize ARNs, which Glue's list APIs omit).
type Client struct {
	clients   map[string]*glue.Client
	regions   []string
	accountID string
}

// NewClient builds per-region Glue clients. When allRegions is true the region
// list is discovered via ec2:DescribeRegions, falling back to the built-in
// list when that call is denied.
func NewClient(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool) (*Client, error) {
	bootstrap := "us-east-1"
	if len(regions) > 0 {
		bootstrap = regions[0]
	}
	base, err := auth.BuildAWSConfig(ctx, awsCfg, bootstrap)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	if allRegions {
		regions = resolveRegions(ctx, base)
	}
	if len(regions) == 0 {
		regions = []string{bootstrap}
	}
	sort.Strings(regions)

	clients := make(map[string]*glue.Client, len(regions))
	for _, r := range regions {
		rCfg := base.Copy()
		rCfg.Region = r
		clients[r] = glue.NewFromConfig(rCfg)
	}
	return &Client{clients: clients, regions: regions, accountID: resolveAccountID(ctx, base)}, nil
}

// resolveAccountID looks up the caller's account ID for ARN synthesis; an
// empty string (when sts:GetCallerIdentity is denied) just yields ARNs with a
// blank account segment, which the console ARN-search fallback still resolves.
func resolveAccountID(ctx context.Context, cfg aws.Config) string {
	out, err := sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		slog.Warn("Unable to resolve account ID for Glue ARNs", "error", err.Error())
		return ""
	}
	return aws.ToString(out.Account)
}

// Regions returns the regions this client queries, sorted.
func (c *Client) Regions() []string { return c.regions }

func (c *Client) clientFor(region string) *glue.Client {
	if cl, ok := c.clients[region]; ok {
		return cl
	}
	for _, cl := range c.clients {
		return cl
	}
	return nil
}

func resolveRegions(ctx context.Context, cfg aws.Config) []string {
	client := awsec2.NewFromConfig(cfg)
	result, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		slog.Warn("Unable to list AWS regions; falling back to the built-in region list",
			"error", err.Error(), "regions", len(awsutil.FallbackRegions))
		return awsutil.FallbackRegions
	}
	var regions []string
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}
	if len(regions) == 0 {
		return awsutil.FallbackRegions
	}
	return regions
}

// LoadInventory fans the per-resource listings out across every region in
// parallel. Per-region failures are soft (opt-in regions commonly deny Glue);
// an error is returned only when every region fails completely.
func (c *Client) LoadInventory(ctx context.Context) (Inventory, error) {
	var (
		mu       sync.Mutex
		inv      Inventory
		firstErr error
		failures int
		wg       sync.WaitGroup
	)

	for _, region := range c.regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			regional, err := c.loadRegion(ctx, region)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("Glue inventory failed", "region", region, "error", err.Error())
				return
			}
			inv.Jobs = append(inv.Jobs, regional.Jobs...)
			inv.Crawlers = append(inv.Crawlers, regional.Crawlers...)
			inv.Triggers = append(inv.Triggers, regional.Triggers...)
			inv.Workflows = append(inv.Workflows, regional.Workflows...)
			inv.Connections = append(inv.Connections, regional.Connections...)
			inv.Databases = append(inv.Databases, regional.Databases...)
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return Inventory{}, firstErr
	}

	inv.sort()
	return inv, nil
}

// loadRegion gathers every resource type for one region. Listings are
// independent and best-effort: a failure in one is logged but the rest proceed,
// so a denied GetTriggers (say) still yields jobs and crawlers.
func (c *Client) loadRegion(ctx context.Context, region string) (Inventory, error) {
	cl := c.clientFor(region)
	var inv Inventory

	jobs, err := c.loadJobs(ctx, cl, region)
	if err != nil {
		slog.Warn("GetJobs failed", "region", region, "error", err.Error())
	}
	inv.Jobs = jobs

	var token *string
	for {
		page, err := cl.GetCrawlers(ctx, &glue.GetCrawlersInput{NextToken: token})
		if err != nil {
			slog.Warn("GetCrawlers failed", "region", region, "error", err.Error())
			break
		}
		for _, cr := range page.Crawlers {
			inv.Crawlers = append(inv.Crawlers, c.mapCrawler(region, cr))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	token = nil
	for {
		page, err := cl.GetTriggers(ctx, &glue.GetTriggersInput{NextToken: token})
		if err != nil {
			slog.Warn("GetTriggers failed", "region", region, "error", err.Error())
			break
		}
		for _, tr := range page.Triggers {
			inv.Triggers = append(inv.Triggers, c.mapTrigger(region, tr))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	token = nil
	for {
		page, err := cl.ListWorkflows(ctx, &glue.ListWorkflowsInput{NextToken: token})
		if err != nil {
			slog.Warn("ListWorkflows failed", "region", region, "error", err.Error())
			break
		}
		for _, name := range page.Workflows {
			inv.Workflows = append(inv.Workflows, Workflow{Name: name, Region: region, ARN: c.arn(region, "workflow/"+name)})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	token = nil
	for {
		page, err := cl.GetConnections(ctx, &glue.GetConnectionsInput{NextToken: token})
		if err != nil {
			slog.Warn("GetConnections failed", "region", region, "error", err.Error())
			break
		}
		for _, conn := range page.ConnectionList {
			inv.Connections = append(inv.Connections, Connection{
				Name: aws.ToString(conn.Name), Region: region,
				ARN:    c.arn(region, "connection/"+aws.ToString(conn.Name)),
				Type:   string(conn.ConnectionType),
				Status: string(conn.Status),
			})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	token = nil
	for {
		page, err := cl.GetDatabases(ctx, &glue.GetDatabasesInput{NextToken: token})
		if err != nil {
			slog.Warn("GetDatabases failed", "region", region, "error", err.Error())
			break
		}
		for _, db := range page.DatabaseList {
			inv.Databases = append(inv.Databases, Database{
				Name: aws.ToString(db.Name), Region: region,
				ARN:         c.arn(region, "database/"+aws.ToString(db.Name)),
				Description: aws.ToString(db.Description),
			})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	return inv, nil
}

// loadJobs lists jobs and stamps each with its latest run state via a
// bounded-concurrency GetJobRuns pass (the same best-effort enrichment the
// collector does, but kept here so the TUI owns its own typed view).
func (c *Client) loadJobs(ctx context.Context, cl *glue.Client, region string) ([]Job, error) {
	var jobs []Job
	var token *string
	for {
		page, err := cl.GetJobs(ctx, &glue.GetJobsInput{NextToken: token})
		if err != nil {
			return jobs, err
		}
		for _, j := range page.Jobs {
			jobs = append(jobs, Job{
				Name: aws.ToString(j.Name), Region: region,
				ARN:         c.arn(region, "job/"+aws.ToString(j.Name)),
				Worker:      workerSummary(j),
				GlueVersion: aws.ToString(j.GlueVersion),
			})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			out, err := cl.GetJobRuns(ctx, &glue.GetJobRunsInput{JobName: aws.String(jobs[i].Name), MaxResults: aws.Int32(1)})
			if err != nil || len(out.JobRuns) == 0 {
				return
			}
			run := out.JobRuns[0]
			jobs[i].LastRunState = string(run.JobRunState)
			jobs[i].LastRunSeconds = run.ExecutionTime
			if run.StartedOn != nil {
				jobs[i].LastRunStarted = *run.StartedOn
			}
		}(i)
	}
	wg.Wait()
	return jobs, nil
}

// JobRuns fetches the most recent run history for a job (newest first).
func (c *Client) JobRuns(ctx context.Context, region, jobName string, limit int32) ([]JobRun, error) {
	out, err := c.clientFor(region).GetJobRuns(ctx, &glue.GetJobRunsInput{
		JobName:    aws.String(jobName),
		MaxResults: aws.Int32(limit),
	})
	if err != nil {
		return nil, err
	}
	runs := make([]JobRun, 0, len(out.JobRuns))
	for _, r := range out.JobRuns {
		jr := JobRun{
			ID:         aws.ToString(r.Id),
			State:      string(r.JobRunState),
			Error:      aws.ToString(r.ErrorMessage),
			ExecSecs:   r.ExecutionTime,
			DPUSeconds: aws.ToFloat64(r.DPUSeconds),
			Worker:     runWorker(r),
			Attempt:    r.Attempt,
		}
		if r.StartedOn != nil {
			jr.Started = *r.StartedOn
		}
		if r.CompletedOn != nil {
			jr.Completed = *r.CompletedOn
		}
		runs = append(runs, jr)
	}
	return runs, nil
}

// JobDefinition fetches a job's full configuration on demand (one GetJob call),
// flattened for the detail panel with secret-looking arguments redacted.
func (c *Client) JobDefinition(ctx context.Context, region, name string) (JobDef, error) {
	out, err := c.clientFor(region).GetJob(ctx, &glue.GetJobInput{JobName: aws.String(name)})
	if err != nil {
		return JobDef{}, err
	}
	if out.Job == nil {
		return JobDef{}, fmt.Errorf("job %q not found", name)
	}
	j := out.Job
	def := JobDef{
		Name: name, Region: region,
		Role:             aws.ToString(j.Role),
		GlueVersion:      aws.ToString(j.GlueVersion),
		ExecutionClass:   string(j.ExecutionClass),
		Worker:           workerSummary(*j),
		TimeoutMinutes:   aws.ToInt32(j.Timeout),
		MaxRetries:       j.MaxRetries,
		SecurityConfig:   aws.ToString(j.SecurityConfiguration),
		BookmarkEnabled:  j.DefaultArguments["--job-bookmark-option"] == "job-bookmark-enable",
		DefaultArguments: redactArgs(j.DefaultArguments),
	}
	if j.Command != nil {
		def.Script = aws.ToString(j.Command.ScriptLocation)
	}
	if j.Connections != nil {
		def.Connections = j.Connections.Connections
	}
	return def, nil
}

func (c *Client) mapCrawler(region string, cr gluetypes.Crawler) Crawler {
	out := Crawler{
		Name: aws.ToString(cr.Name), Region: region,
		ARN:      c.arn(region, "crawler/"+aws.ToString(cr.Name)),
		State:    string(cr.State),
		Database: aws.ToString(cr.DatabaseName),
	}
	if cr.LastCrawl != nil {
		out.LastCrawlStatus = string(cr.LastCrawl.Status)
	}
	if cr.Schedule != nil {
		out.Schedule = aws.ToString(cr.Schedule.ScheduleExpression)
	}
	return out
}

func (c *Client) mapTrigger(region string, tr gluetypes.Trigger) Trigger {
	return Trigger{
		Name: aws.ToString(tr.Name), Region: region,
		ARN:      c.arn(region, "trigger/"+aws.ToString(tr.Name)),
		Type:     string(tr.Type),
		State:    string(tr.State),
		Schedule: aws.ToString(tr.Schedule),
		Workflow: aws.ToString(tr.WorkflowName),
	}
}

// arn synthesizes a Glue ARN to match the form the Tagging API emits.
func (c *Client) arn(region, resource string) string {
	return fmt.Sprintf("arn:aws:glue:%s:%s:%s", region, c.accountID, resource)
}

func (inv *Inventory) sort() {
	sort.Slice(inv.Jobs, func(i, j int) bool {
		return less(inv.Jobs[i].Name, inv.Jobs[i].Region, inv.Jobs[j].Name, inv.Jobs[j].Region)
	})
	sort.Slice(inv.Crawlers, func(i, j int) bool {
		return less(inv.Crawlers[i].Name, inv.Crawlers[i].Region, inv.Crawlers[j].Name, inv.Crawlers[j].Region)
	})
	sort.Slice(inv.Triggers, func(i, j int) bool {
		return less(inv.Triggers[i].Name, inv.Triggers[i].Region, inv.Triggers[j].Name, inv.Triggers[j].Region)
	})
	sort.Slice(inv.Workflows, func(i, j int) bool {
		return less(inv.Workflows[i].Name, inv.Workflows[i].Region, inv.Workflows[j].Name, inv.Workflows[j].Region)
	})
	sort.Slice(inv.Connections, func(i, j int) bool {
		return less(inv.Connections[i].Name, inv.Connections[i].Region, inv.Connections[j].Name, inv.Connections[j].Region)
	})
	sort.Slice(inv.Databases, func(i, j int) bool {
		return less(inv.Databases[i].Name, inv.Databases[i].Region, inv.Databases[j].Name, inv.Databases[j].Region)
	})
}

func less(ni, ri, nj, rj string) bool {
	if ni != nj {
		return ni < nj
	}
	return ri < rj
}

// workerSummary renders a job's capacity as "G.1X ×10", falling back to a DPU
// figure for older MaxCapacity-style jobs.
func workerSummary(j gluetypes.Job) string {
	if wt := string(j.WorkerType); wt != "" {
		return fmt.Sprintf("%s ×%d", wt, aws.ToInt32(j.NumberOfWorkers))
	}
	if mc := aws.ToFloat64(j.MaxCapacity); mc > 0 {
		return fmt.Sprintf("%g DPU", mc)
	}
	return ""
}

func runWorker(r gluetypes.JobRun) string {
	if wt := string(r.WorkerType); wt != "" {
		return fmt.Sprintf("%s ×%d", wt, aws.ToInt32(r.NumberOfWorkers))
	}
	if mc := aws.ToFloat64(r.MaxCapacity); mc > 0 {
		return fmt.Sprintf("%g DPU", mc)
	}
	return ""
}
