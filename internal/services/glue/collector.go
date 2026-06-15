// Package glue collects Glue databases, jobs, crawlers, triggers, workflows and
// connections. A typed collector is needed because the Resource Groups Tagging
// API only returns tagged resources; untagged Glue resources are invisible to
// the broad discovery sweep.
//
// Beyond bare inventory the collector stamps each resource with the operational
// facts engineers ask for first — a job's latest run state/duration, a crawler's
// last-crawl status, a trigger's type/schedule — so the summary TUI and the
// dedicated Glue dashboard can show health at a glance (AXE-025).
package glue

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// runConcurrency bounds parallel GetJobRuns calls so accounts with many jobs
// don't serialize on per-job run lookups or trip Glue's request throttling.
const runConcurrency = 8

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "glue" }

func (c *Collector) IsGlobal() bool { return false }

// Collect gathers databases, jobs, crawlers, triggers, workflows and
// connections. They are independent listings, so a failure in one is recorded
// but does not stop the others (partial results plus a joined error), matching
// the best-effort collector contract.
func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := glue.NewFromConfig(input.AWSConfig)
	region, account := input.Region, input.AccountID

	var resources []model.Resource
	var errs []error

	dbs, err := c.collectDatabases(ctx, client, region, account)
	resources = input.EmitOrAppend(resources, dbs)
	errs = append(errs, err)

	jobs, err := c.collectJobs(ctx, client, input, region, account)
	resources = input.EmitOrAppend(resources, jobs)
	errs = append(errs, err)

	crawlers, err := c.collectCrawlers(ctx, client, region, account)
	resources = input.EmitOrAppend(resources, crawlers)
	errs = append(errs, err)

	triggers, err := c.collectTriggers(ctx, client, region, account)
	resources = input.EmitOrAppend(resources, triggers)
	errs = append(errs, err)

	workflows, err := c.collectWorkflows(ctx, client, region, account)
	resources = input.EmitOrAppend(resources, workflows)
	errs = append(errs, err)

	conns, err := c.collectConnections(ctx, client, region, account)
	resources = input.EmitOrAppend(resources, conns)
	errs = append(errs, err)

	return resources, errors.Join(errs...)
}

func (c *Collector) collectDatabases(ctx context.Context, client *glue.Client, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetDatabases(ctx, &glue.GetDatabasesInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue databases: %w", err)
		}
		for _, db := range page.DatabaseList {
			out = append(out, mapDatabase(region, account, db))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectJobs(ctx context.Context, client *glue.Client, input services.CollectInput, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetJobs(ctx, &glue.GetJobsInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue jobs: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Jobs))
		for _, j := range page.Jobs {
			batch = append(batch, mapJob(region, account, j, input.DetailLevel))
		}
		// GetJobs returns the definition but not run history; stamp each job
		// with its latest run state, fetched concurrently (best-effort — a
		// denied/failed GetJobRuns leaves the job listed without run state).
		c.applyLatestRun(ctx, client, batch)
		out = append(out, batch...)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

// applyLatestRun fills each job's State and last-run Summary fields from the
// most recent GetJobRuns entry. Each goroutine writes its own slice index, so
// no locking is needed; errors are swallowed because run state is an
// enrichment, not a reason to drop the job.
func (c *Collector) applyLatestRun(ctx context.Context, client *glue.Client, batch []model.Resource) {
	var g errgroup.Group
	g.SetLimit(runConcurrency)
	for i := range batch {
		if batch[i].Name == "" {
			continue
		}
		g.Go(func() error {
			out, err := client.GetJobRuns(ctx, &glue.GetJobRunsInput{
				JobName:    aws.String(batch[i].Name),
				MaxResults: aws.Int32(1),
			})
			if err != nil || len(out.JobRuns) == 0 {
				return nil
			}
			applyRunSummary(&batch[i], out.JobRuns[0])
			return nil
		})
	}
	_ = g.Wait()
}

func (c *Collector) collectCrawlers(ctx context.Context, client *glue.Client, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetCrawlers(ctx, &glue.GetCrawlersInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue crawlers: %w", err)
		}
		for _, cr := range page.Crawlers {
			out = append(out, mapCrawler(region, account, cr))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectTriggers(ctx context.Context, client *glue.Client, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetTriggers(ctx, &glue.GetTriggersInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue triggers: %w", err)
		}
		for _, tr := range page.Triggers {
			out = append(out, mapTrigger(region, account, tr))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectWorkflows(ctx context.Context, client *glue.Client, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.ListWorkflows(ctx, &glue.ListWorkflowsInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue workflows: %w", err)
		}
		for _, name := range page.Workflows {
			out = append(out, model.Resource{
				Service: "glue", Type: "workflow", Region: region,
				ID: name, Name: name,
				ARN: arn(region, account, "workflow/"+name),
			})
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) collectConnections(ctx context.Context, client *glue.Client, region, account string) ([]model.Resource, error) {
	var out []model.Resource
	var token *string
	for {
		page, err := client.GetConnections(ctx, &glue.GetConnectionsInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list Glue connections: %w", err)
		}
		for _, conn := range page.ConnectionList {
			out = append(out, mapConnection(region, account, conn))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

// --- pure mapping helpers (fixture-tested) ---------------------------------

func mapDatabase(region, account string, db types.Database) model.Resource {
	name := aws.ToString(db.Name)
	res := model.Resource{
		Service: "glue", Type: "database", Region: region,
		ID: name, Name: name, CreatedAt: db.CreateTime,
		ARN: arn(region, account, "database/"+name),
	}
	if d := aws.ToString(db.Description); d != "" {
		res.Summary = map[string]string{"description": d}
	}
	return res
}

// mapJob maps a job definition to a resource. Run state is layered on later by
// applyRunSummary; the definition itself (version, worker, role, script) is
// available directly from GetJobs.
func mapJob(region, account string, j types.Job, detail services.DetailLevel) model.Resource {
	name := aws.ToString(j.Name)
	res := model.Resource{
		Service: "glue", Type: "job", Region: region,
		ID: name, Name: name, CreatedAt: j.CreatedOn,
		ARN:     arn(region, account, "job/"+name),
		Summary: map[string]string{},
	}
	if w := workerSummary(j); w != "" {
		res.Summary["worker"] = w
	}
	if v := aws.ToString(j.GlueVersion); v != "" {
		res.Summary["glueVersion"] = v
	}
	if len(res.Summary) == 0 {
		res.Summary = nil
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		var script string
		if j.Command != nil {
			script = aws.ToString(j.Command.ScriptLocation)
		}
		res.Details = map[string]any{
			"role":               aws.ToString(j.Role),
			"glueVersion":        aws.ToString(j.GlueVersion),
			"executionClass":     string(j.ExecutionClass),
			"workerType":         string(j.WorkerType),
			"numberOfWorkers":    aws.ToInt32(j.NumberOfWorkers),
			"maxCapacity":        aws.ToFloat64(j.MaxCapacity),
			"timeoutMinutes":     aws.ToInt32(j.Timeout),
			"maxRetries":         j.MaxRetries,
			"script":             script,
			"securityConfig":     aws.ToString(j.SecurityConfiguration),
			"connections":        connectionNames(j.Connections),
			"jobBookmarkEnabled": bookmarkEnabled(j.DefaultArguments),
			"defaultArguments":   redactArgs(j.DefaultArguments),
		}
	}
	return res
}

// applyRunSummary layers a job's most-recent run onto its resource: State holds
// the run state and the Summary carries the start time and execution seconds.
func applyRunSummary(res *model.Resource, run types.JobRun) {
	res.State = string(run.JobRunState)
	if res.Summary == nil {
		res.Summary = map[string]string{}
	}
	res.Summary["lastRunState"] = string(run.JobRunState)
	if run.StartedOn != nil {
		res.Summary["lastRunStarted"] = run.StartedOn.UTC().Format("2006-01-02T15:04:05Z")
	}
	if run.ExecutionTime > 0 {
		res.Summary["lastRunSeconds"] = strconv.Itoa(int(run.ExecutionTime))
	}
}

func mapCrawler(region, account string, cr types.Crawler) model.Resource {
	name := aws.ToString(cr.Name)
	res := model.Resource{
		Service: "glue", Type: "crawler", Region: region,
		ID: name, Name: name, State: string(cr.State),
		CreatedAt: cr.CreationTime,
		ARN:       arn(region, account, "crawler/"+name),
		Summary:   map[string]string{},
	}
	if db := aws.ToString(cr.DatabaseName); db != "" {
		res.Summary["database"] = db
	}
	if cr.LastCrawl != nil {
		res.Summary["lastCrawlStatus"] = string(cr.LastCrawl.Status)
		if cr.LastCrawl.StartTime != nil {
			res.Summary["lastCrawlStarted"] = cr.LastCrawl.StartTime.UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	if cr.Schedule != nil {
		if expr := aws.ToString(cr.Schedule.ScheduleExpression); expr != "" {
			res.Summary["schedule"] = expr
		}
	}
	if len(res.Summary) == 0 {
		res.Summary = nil
	}
	return res
}

func mapTrigger(region, account string, tr types.Trigger) model.Resource {
	name := aws.ToString(tr.Name)
	res := model.Resource{
		Service: "glue", Type: "trigger", Region: region,
		ID: name, Name: name, State: string(tr.State),
		ARN:     arn(region, account, "trigger/"+name),
		Summary: map[string]string{"type": string(tr.Type)},
	}
	if s := aws.ToString(tr.Schedule); s != "" {
		res.Summary["schedule"] = s
	}
	if wf := aws.ToString(tr.WorkflowName); wf != "" {
		res.Summary["workflow"] = wf
	}
	return res
}

func mapConnection(region, account string, conn types.Connection) model.Resource {
	name := aws.ToString(conn.Name)
	res := model.Resource{
		Service: "glue", Type: "connection", Region: region,
		ID: name, Name: name, State: string(conn.Status),
		CreatedAt: conn.CreationTime,
		ARN:       arn(region, account, "connection/"+name),
		Summary:   map[string]string{"connectionType": string(conn.ConnectionType)},
	}
	if d := aws.ToString(conn.Description); d != "" {
		res.Summary["description"] = d
	}
	return res
}

// workerSummary renders a job's capacity allocation as "G.1X ×10", falling back
// to a DPU figure for older MaxCapacity-style jobs.
func workerSummary(j types.Job) string {
	if wt := string(j.WorkerType); wt != "" {
		return fmt.Sprintf("%s ×%d", wt, aws.ToInt32(j.NumberOfWorkers))
	}
	if mc := aws.ToFloat64(j.MaxCapacity); mc > 0 {
		return fmt.Sprintf("%g DPU", mc)
	}
	return ""
}

func connectionNames(cl *types.ConnectionsList) []string {
	if cl == nil {
		return nil
	}
	return cl.Connections
}

// bookmarkEnabled reports whether the job's default arguments enable bookmarks.
// AWS represents this with the --job-bookmark-option argument.
func bookmarkEnabled(args map[string]string) bool {
	return args["--job-bookmark-option"] == "job-bookmark-enable"
}

// redactArgs copies default arguments, masking values whose key looks like a
// secret so credentials passed as job arguments never land in output.
func redactArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for k, v := range args {
		if isSecretKey(k) {
			out[k] = "***"
		} else {
			out[k] = v
		}
	}
	return out
}

func isSecretKey(k string) bool {
	for _, needle := range []string{"secret", "password", "passwd", "token", "credential", "apikey", "api_key"} {
		if containsFold(k, needle) {
			return true
		}
	}
	return false
}

// containsFold reports whether s contains substr, case-insensitively (ASCII
// keys only, which AWS argument names are).
func containsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if equalFoldASCII(s[i:i+len(substr)], substr) {
			return true
		}
	}
	return false
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// arn builds a Glue resource ARN. Glue's list APIs return no ARNs, so they are
// constructed to match the form the Tagging API emits so the two merge.
func arn(region, account, resource string) string {
	return fmt.Sprintf("arn:aws:glue:%s:%s:%s", region, account, resource)
}
