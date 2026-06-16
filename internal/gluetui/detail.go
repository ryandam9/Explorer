package gluetui

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue"
	gluetypes "github.com/aws/aws-sdk-go-v2/service/glue/types"
)

// DetailRow is one line in a resource-detail overlay. Its rendering depends on
// which fields are set (see detailBody):
//   - Section        → a section header
//   - Label (+Value) → an aligned "label   value" line (em dash when empty)
//   - Value only     → an indented bullet/continuation line
//   - all empty      → a blank separator line
type DetailRow struct {
	Label   string
	Value   string
	Section bool
}

// ResourceDetail is an on-demand, flattened view of a single non-job Glue
// resource (crawler, trigger, workflow, connection or database), shown in the
// detail overlay opened with Enter. Secret-looking values are redacted.
type ResourceDetail struct {
	Title string
	Rows  []DetailRow
}

// detailBuilder accumulates DetailRows; kv/header/bullet keep each *Detail
// method terse and consistent.
type detailBuilder struct{ rows []DetailRow }

func (d *detailBuilder) kv(label, value string) {
	d.rows = append(d.rows, DetailRow{Label: label, Value: value})
}
func (d *detailBuilder) header(label string) {
	d.rows = append(d.rows, DetailRow{}, DetailRow{Label: label, Section: true})
}
func (d *detailBuilder) bullet(value string) { d.rows = append(d.rows, DetailRow{Value: value}) }

// detailTitleFor labels the overlay before the on-demand fetch returns (so the
// box has a title while it loads).
func detailTitleFor(typ, name string) string {
	caser := map[string]string{
		"crawler":    "Crawler",
		"trigger":    "Trigger",
		"workflow":   "Workflow",
		"connection": "Connection",
		"database":   "Database",
	}
	label := caser[typ]
	if label == "" {
		label = "Detail"
	}
	return label + " — " + name
}

// CrawlerDetail fetches one crawler's full configuration (one GetCrawler call),
// flattened for the detail overlay.
func (c *Client) CrawlerDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetCrawler(ctx, &glue.GetCrawlerInput{Name: aws.String(name)})
	if err != nil {
		return ResourceDetail{}, err
	}
	cr := out.Crawler
	if cr == nil {
		return ResourceDetail{}, fmt.Errorf("crawler %q not found", name)
	}
	var d detailBuilder
	d.kv("State", string(cr.State))
	d.kv("Database", aws.ToString(cr.DatabaseName))
	d.kv("Description", aws.ToString(cr.Description))
	d.kv("IAM role", aws.ToString(cr.Role))
	d.kv("Table prefix", aws.ToString(cr.TablePrefix))
	if cr.Schedule != nil {
		d.kv("Schedule", aws.ToString(cr.Schedule.ScheduleExpression))
	}
	d.kv("Security config", aws.ToString(cr.CrawlerSecurityConfiguration))
	if len(cr.Classifiers) > 0 {
		d.kv("Classifiers", strings.Join(cr.Classifiers, ", "))
	}
	d.kv("Created", fmtTimePtr(cr.CreationTime))
	d.kv("Last updated", fmtTimePtr(cr.LastUpdated))
	d.kv("Version", fmt.Sprintf("%d", cr.Version))

	if tgts := crawlerTargets(cr.Targets); len(tgts) > 0 {
		d.header("Targets")
		for _, t := range tgts {
			d.bullet(t)
		}
	}
	if lc := cr.LastCrawl; lc != nil {
		d.header("Last crawl")
		d.kv("  Status", string(lc.Status))
		d.kv("  Started", fmtTimePtr(lc.StartTime))
		if msg := aws.ToString(lc.ErrorMessage); msg != "" {
			d.kv("  Error", msg)
		}
		if lg := aws.ToString(lc.LogGroup); lg != "" {
			d.kv("  Log group", lg)
		}
	}
	return ResourceDetail{Title: detailTitleFor("crawler", name), Rows: d.rows}, nil
}

// crawlerTargets summarizes a crawler's heterogeneous target set as one line per
// data store, e.g. "s3: s3://bucket/prefix".
func crawlerTargets(t *gluetypes.CrawlerTargets) []string {
	if t == nil {
		return nil
	}
	var out []string
	for _, s := range t.S3Targets {
		out = append(out, "s3: "+aws.ToString(s.Path))
	}
	for _, j := range t.JdbcTargets {
		out = append(out, "jdbc: "+aws.ToString(j.Path))
	}
	for _, dt := range t.DynamoDBTargets {
		out = append(out, "dynamodb: "+aws.ToString(dt.Path))
	}
	for _, ct := range t.CatalogTargets {
		out = append(out, "catalog: "+aws.ToString(ct.DatabaseName))
	}
	for _, mt := range t.MongoDBTargets {
		out = append(out, "mongodb: "+aws.ToString(mt.Path))
	}
	for _, dt := range t.DeltaTargets {
		out = append(out, "delta: "+strings.Join(dt.DeltaTables, ", "))
	}
	for _, it := range t.IcebergTargets {
		out = append(out, "iceberg: "+strings.Join(it.Paths, ", "))
	}
	for _, ht := range t.HudiTargets {
		out = append(out, "hudi: "+strings.Join(ht.Paths, ", "))
	}
	return out
}

// TriggerDetail fetches one trigger's configuration (one GetTrigger call).
func (c *Client) TriggerDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetTrigger(ctx, &glue.GetTriggerInput{Name: aws.String(name)})
	if err != nil {
		return ResourceDetail{}, err
	}
	tr := out.Trigger
	if tr == nil {
		return ResourceDetail{}, fmt.Errorf("trigger %q not found", name)
	}
	var d detailBuilder
	d.kv("Type", string(tr.Type))
	d.kv("State", string(tr.State))
	d.kv("Schedule", aws.ToString(tr.Schedule))
	d.kv("Workflow", aws.ToString(tr.WorkflowName))
	d.kv("Description", aws.ToString(tr.Description))

	if len(tr.Actions) > 0 {
		d.header("Actions")
		for _, a := range tr.Actions {
			switch {
			case a.JobName != nil:
				d.bullet("job: " + aws.ToString(a.JobName))
			case a.CrawlerName != nil:
				d.bullet("crawler: " + aws.ToString(a.CrawlerName))
			}
		}
	}
	if p := tr.Predicate; p != nil && len(p.Conditions) > 0 {
		head := "Conditions"
		if p.Logical != "" {
			head = "Conditions (" + string(p.Logical) + ")"
		}
		d.header(head)
		for _, cond := range p.Conditions {
			switch {
			case cond.JobName != nil:
				d.bullet(fmt.Sprintf("job %s → %s", aws.ToString(cond.JobName), string(cond.State)))
			case cond.CrawlerName != nil:
				d.bullet(fmt.Sprintf("crawler %s → %s", aws.ToString(cond.CrawlerName), string(cond.CrawlState)))
			}
		}
	}
	return ResourceDetail{Title: detailTitleFor("trigger", name), Rows: d.rows}, nil
}

// WorkflowDetail fetches one workflow's configuration and last-run summary (one
// GetWorkflow call).
func (c *Client) WorkflowDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetWorkflow(ctx, &glue.GetWorkflowInput{
		Name:         aws.String(name),
		IncludeGraph: aws.Bool(false),
	})
	if err != nil {
		return ResourceDetail{}, err
	}
	wf := out.Workflow
	if wf == nil {
		return ResourceDetail{}, fmt.Errorf("workflow %q not found", name)
	}
	var d detailBuilder
	d.kv("Description", aws.ToString(wf.Description))
	d.kv("Max concurrent runs", fmt.Sprintf("%d", aws.ToInt32(wf.MaxConcurrentRuns)))
	d.kv("Created", fmtTimePtr(wf.CreatedOn))
	d.kv("Last modified", fmtTimePtr(wf.LastModifiedOn))

	if len(wf.DefaultRunProperties) > 0 {
		d.header("Default run properties")
		props := redactArgs(wf.DefaultRunProperties)
		for _, k := range sortedKeys(props) {
			d.bullet(fmt.Sprintf("%s = %s", k, props[k]))
		}
	}
	if lr := wf.LastRun; lr != nil {
		d.header("Last run")
		d.kv("  Status", string(lr.Status))
		d.kv("  Started", fmtTimePtr(lr.StartedOn))
		d.kv("  Completed", fmtTimePtr(lr.CompletedOn))
		if s := lr.Statistics; s != nil {
			d.kv("  Actions", fmt.Sprintf("%d total · %d succeeded · %d failed · %d running",
				s.TotalActions, s.SucceededActions, s.FailedActions, s.RunningActions))
		}
		if msg := aws.ToString(lr.ErrorMessage); msg != "" {
			d.kv("  Error", msg)
		}
	}
	return ResourceDetail{Title: detailTitleFor("workflow", name), Rows: d.rows}, nil
}

// ConnectionDetail fetches one connection's configuration (one GetConnection
// call). Connection properties are rendered with secret-looking values redacted.
func (c *Client) ConnectionDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetConnection(ctx, &glue.GetConnectionInput{Name: aws.String(name)})
	if err != nil {
		return ResourceDetail{}, err
	}
	conn := out.Connection
	if conn == nil {
		return ResourceDetail{}, fmt.Errorf("connection %q not found", name)
	}
	var d detailBuilder
	d.kv("Type", string(conn.ConnectionType))
	d.kv("Status", string(conn.Status))
	if reason := aws.ToString(conn.StatusReason); reason != "" {
		d.kv("Status reason", reason)
	}
	d.kv("Description", aws.ToString(conn.Description))
	d.kv("Created", fmtTimePtr(conn.CreationTime))
	d.kv("Last updated", fmtTimePtr(conn.LastUpdatedTime))

	if pcr := conn.PhysicalConnectionRequirements; pcr != nil {
		d.header("Network")
		d.kv("  Subnet", aws.ToString(pcr.SubnetId))
		d.kv("  Availability zone", aws.ToString(pcr.AvailabilityZone))
		if len(pcr.SecurityGroupIdList) > 0 {
			d.kv("  Security groups", strings.Join(pcr.SecurityGroupIdList, ", "))
		}
	}
	if len(conn.ConnectionProperties) > 0 {
		d.header("Properties (secrets redacted)")
		props := redactArgs(conn.ConnectionProperties)
		for _, k := range sortedKeys(props) {
			d.bullet(fmt.Sprintf("%s = %s", k, props[k]))
		}
	}
	return ResourceDetail{Title: detailTitleFor("connection", name), Rows: d.rows}, nil
}

// DatabaseDetail fetches one catalog database's configuration (one GetDatabase
// call).
func (c *Client) DatabaseDetail(ctx context.Context, region, name string) (ResourceDetail, error) {
	out, err := c.clientFor(region).GetDatabase(ctx, &glue.GetDatabaseInput{Name: aws.String(name)})
	if err != nil {
		return ResourceDetail{}, err
	}
	db := out.Database
	if db == nil {
		return ResourceDetail{}, fmt.Errorf("database %q not found", name)
	}
	var d detailBuilder
	d.kv("Description", aws.ToString(db.Description))
	d.kv("Location URI", aws.ToString(db.LocationUri))
	d.kv("Catalog ID", aws.ToString(db.CatalogId))
	d.kv("Created", fmtTimePtr(db.CreateTime))
	if ld := db.TargetDatabase; ld != nil {
		d.kv("Shared from", aws.ToString(ld.CatalogId)+"/"+aws.ToString(ld.DatabaseName))
	}
	if len(db.Parameters) > 0 {
		d.header("Parameters")
		params := redactArgs(db.Parameters)
		for _, k := range sortedKeys(params) {
			d.bullet(fmt.Sprintf("%s = %s", k, params[k]))
		}
	}
	return ResourceDetail{Title: detailTitleFor("database", name), Rows: d.rows}, nil
}
