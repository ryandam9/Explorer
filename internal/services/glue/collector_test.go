package glue

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/glue/types"

	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "glue" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestARN(t *testing.T) {
	if got := arn("us-east-1", "123456789012", "job/etl"); got != "arn:aws:glue:us-east-1:123456789012:job/etl" {
		t.Errorf("arn = %q", got)
	}
	if got := arn("eu-west-1", "1", "database/sales"); got != "arn:aws:glue:eu-west-1:1:database/sales" {
		t.Errorf("arn = %q", got)
	}
}

func TestMapJobSummaryAndDefinition(t *testing.T) {
	j := types.Job{
		Name:            aws.String("nightly-orders-etl"),
		GlueVersion:     aws.String("4.0"),
		WorkerType:      types.WorkerTypeG1x,
		NumberOfWorkers: aws.Int32(10),
		Role:            aws.String("arn:aws:iam::1:role/glue"),
		Command:         &types.JobCommand{ScriptLocation: aws.String("s3://scripts/etl.py")},
		Connections:     &types.ConnectionsList{Connections: []string{"prod-redshift"}},
		Timeout:         aws.Int32(2880),
		MaxRetries:      1,
		DefaultArguments: map[string]string{
			"--job-bookmark-option": "job-bookmark-enable",
			"--TempDir":             "s3://tmp/",
			"--db-password":         "hunter2",
		},
	}

	// Summary-level: headline facts, no Details blob.
	res := mapJob("us-east-1", "1", j, services.DetailLevelSummary)
	if res.Type != "job" || res.Name != "nightly-orders-etl" {
		t.Fatalf("unexpected base mapping: %+v", res)
	}
	if res.ARN != "arn:aws:glue:us-east-1:1:job/nightly-orders-etl" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if got := res.Summary["worker"]; got != "G.1X ×10" {
		t.Errorf("worker summary = %q", got)
	}
	if got := res.Summary["glueVersion"]; got != "4.0" {
		t.Errorf("glueVersion = %q", got)
	}
	if res.Details != nil {
		t.Errorf("summary level should carry no Details, got %v", res.Details)
	}

	// Detailed-level: full definition, with secret args redacted.
	det := mapJob("us-east-1", "1", j, services.DetailLevelDetailed)
	if det.Details == nil {
		t.Fatal("detailed level should carry Details")
	}
	if det.Details["script"] != "s3://scripts/etl.py" {
		t.Errorf("script = %v", det.Details["script"])
	}
	if det.Details["jobBookmarkEnabled"] != true {
		t.Errorf("jobBookmarkEnabled = %v", det.Details["jobBookmarkEnabled"])
	}
	args, ok := det.Details["defaultArguments"].(map[string]string)
	if !ok {
		t.Fatalf("defaultArguments type = %T", det.Details["defaultArguments"])
	}
	if args["--db-password"] != "***" {
		t.Errorf("secret arg not redacted: %q", args["--db-password"])
	}
	if args["--TempDir"] != "s3://tmp/" {
		t.Errorf("non-secret arg altered: %q", args["--TempDir"])
	}
}

func TestWorkerSummaryFallbacks(t *testing.T) {
	maxCap := mapJob("r", "a", types.Job{Name: aws.String("legacy"), MaxCapacity: aws.Float64(10)}, services.DetailLevelSummary)
	if got := maxCap.Summary["worker"]; got != "10 DPU" {
		t.Errorf("MaxCapacity worker = %q, want %q", got, "10 DPU")
	}
	none := mapJob("r", "a", types.Job{Name: aws.String("bare")}, services.DetailLevelSummary)
	if none.Summary != nil {
		t.Errorf("bare job should have nil Summary, got %v", none.Summary)
	}
}

func TestApplyRunSummary(t *testing.T) {
	started := time.Date(2026, 6, 15, 1, 14, 0, 0, time.UTC)
	res := mapJob("r", "a", types.Job{Name: aws.String("j"), GlueVersion: aws.String("4.0")}, services.DetailLevelSummary)
	applyRunSummary(&res, types.JobRun{
		JobRunState:   types.JobRunStateFailed,
		StartedOn:     &started,
		ExecutionTime: 742,
	})
	if res.State != "FAILED" {
		t.Errorf("State = %q", res.State)
	}
	if res.Summary["lastRunState"] != "FAILED" {
		t.Errorf("lastRunState = %q", res.Summary["lastRunState"])
	}
	if res.Summary["lastRunStarted"] != "2026-06-15T01:14:00Z" {
		t.Errorf("lastRunStarted = %q", res.Summary["lastRunStarted"])
	}
	if res.Summary["lastRunSeconds"] != "742" {
		t.Errorf("lastRunSeconds = %q", res.Summary["lastRunSeconds"])
	}
}

func TestMapCrawler(t *testing.T) {
	started := time.Date(2026, 6, 14, 3, 0, 0, 0, time.UTC)
	cr := types.Crawler{
		Name:         aws.String("orders-crawler"),
		State:        types.CrawlerStateReady,
		DatabaseName: aws.String("sales"),
		LastCrawl:    &types.LastCrawlInfo{Status: types.LastCrawlStatusSucceeded, StartTime: &started},
		Schedule:     &types.Schedule{ScheduleExpression: aws.String("cron(0 3 * * ? *)")},
	}
	res := mapCrawler("us-east-1", "1", cr)
	if res.Type != "crawler" || res.State != "READY" {
		t.Fatalf("unexpected crawler mapping: %+v", res)
	}
	if res.Summary["database"] != "sales" {
		t.Errorf("database = %q", res.Summary["database"])
	}
	if res.Summary["lastCrawlStatus"] != "SUCCEEDED" {
		t.Errorf("lastCrawlStatus = %q", res.Summary["lastCrawlStatus"])
	}
	if res.Summary["schedule"] != "cron(0 3 * * ? *)" {
		t.Errorf("schedule = %q", res.Summary["schedule"])
	}
}

func TestMapTriggerAndConnection(t *testing.T) {
	tr := mapTrigger("r", "a", types.Trigger{
		Name:         aws.String("nightly"),
		Type:         types.TriggerTypeScheduled,
		State:        types.TriggerStateActivated,
		Schedule:     aws.String("cron(0 1 * * ? *)"),
		WorkflowName: aws.String("orders-wf"),
	})
	if tr.Type != "trigger" || tr.State != "ACTIVATED" {
		t.Fatalf("trigger mapping: %+v", tr)
	}
	if tr.Summary["type"] != "SCHEDULED" || tr.Summary["workflow"] != "orders-wf" {
		t.Errorf("trigger summary = %v", tr.Summary)
	}

	conn := mapConnection("r", "a", types.Connection{
		Name:           aws.String("prod-redshift"),
		ConnectionType: types.ConnectionTypeJdbc,
	})
	if conn.Type != "connection" || conn.Summary["connectionType"] != "JDBC" {
		t.Errorf("connection mapping: %+v", conn)
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"--db-password", "--API_KEY", "--MySecretArg", "--auth-token"} {
		if !isSecretKey(k) {
			t.Errorf("%q should be detected as secret", k)
		}
	}
	for _, k := range []string{"--TempDir", "--job-language", "--enable-metrics"} {
		if isSecretKey(k) {
			t.Errorf("%q should not be detected as secret", k)
		}
	}
}
