package findings

import (
	"testing"
	"time"
)

func byID(fs []Finding) map[string]Finding {
	m := make(map[string]Finding, len(fs))
	for _, f := range fs {
		m[f.ID] = f
	}
	return m
}

// healthyJob is a clean baseline: secure, recently and successfully run, modest
// workers. Tests mutate a copy to trigger a single check.
func healthyJob() GlueJob {
	return GlueJob{
		Name: "etl", ARN: "arn:aws:glue:us-east-1::job/etl",
		HasSecurityConfig: true, NumberOfWorkers: 2, RunsKnown: true,
		Runs: []GlueRun{{State: "SUCCEEDED", Started: time.Now().Add(-1 * time.Hour), ExecSecs: 600, DPUSeconds: 1000}},
	}
}

func analyzeJob(j GlueJob) map[string]Finding {
	return byID(AnalyzeGlue(GlueSnapshot{Region: "us-east-1", Now: time.Now(), Jobs: []GlueJob{j}}))
}

func TestAnalyzeGlue_CleanNoFindings(t *testing.T) {
	snap := GlueSnapshot{
		Region: "us-east-1", Now: time.Now(),
		Jobs:     []GlueJob{healthyJob()},
		Crawlers: []GlueCrawler{{Name: "c", State: "READY", LastCrawlStatus: "SUCCEEDED"}},
	}
	if fs := AnalyzeGlue(snap); len(fs) != 0 {
		t.Errorf("clean snapshot produced findings: %+v", fs)
	}
}

func TestGlueNoSecurityConfig(t *testing.T) {
	j := healthyJob()
	j.HasSecurityConfig = false
	if _, ok := analyzeJob(j)[CheckGlueNoSecurityConf]; !ok {
		t.Error("expected GLU-SEC-001")
	}
	if _, ok := analyzeJob(healthyJob())[CheckGlueNoSecurityConf]; ok {
		t.Error("secure job should not flag GLU-SEC-001")
	}
}

func TestGlueAllRunsFailed(t *testing.T) {
	j := healthyJob()
	j.Runs = []GlueRun{
		{State: "FAILED", Started: time.Now()},
		{State: "TIMEOUT", Started: time.Now().Add(-time.Hour)},
		{State: "FAILED", Started: time.Now().Add(-2 * time.Hour)},
	}
	got := analyzeJob(j)
	if _, ok := got[CheckGlueAllRunsFailed]; !ok {
		t.Error("expected GLU-JOB-001 for a 3-run failure streak")
	}
	// The streak is the louder signal — latest-failed must not also fire.
	if _, ok := got[CheckGlueLastRunFailed]; ok {
		t.Error("GLU-JOB-003 should not fire alongside the streak")
	}
}

func TestGlueLatestRunFailedOnly(t *testing.T) {
	j := healthyJob()
	j.Runs = []GlueRun{
		{State: "FAILED", Started: time.Now()},
		{State: "SUCCEEDED", Started: time.Now().Add(-time.Hour)},
	}
	got := analyzeJob(j)
	if _, ok := got[CheckGlueLastRunFailed]; !ok {
		t.Error("expected GLU-JOB-003 for a single latest failure")
	}
	if _, ok := got[CheckGlueAllRunsFailed]; ok {
		t.Error("GLU-JOB-001 should not fire without a streak")
	}
}

func TestGlueJobStale(t *testing.T) {
	never := healthyJob()
	never.Runs = nil
	if _, ok := analyzeJob(never)[CheckGlueJobStale]; !ok {
		t.Error("expected GLU-JOB-002 for a never-run job")
	}

	old := healthyJob()
	old.Runs = []GlueRun{{State: "SUCCEEDED", Started: time.Now().Add(-40 * 24 * time.Hour), ExecSecs: 600}}
	if _, ok := analyzeJob(old)[CheckGlueJobStale]; !ok {
		t.Error("expected GLU-JOB-002 for a job last run 40 days ago")
	}
	if _, ok := analyzeJob(healthyJob())[CheckGlueJobStale]; ok {
		t.Error("recently-run job should not flag GLU-JOB-002")
	}
}

func TestGlueRunsUnknownStaysSilent(t *testing.T) {
	j := healthyJob()
	j.RunsKnown = false // GetJobRuns was denied
	j.Runs = nil
	got := analyzeJob(j)
	for _, id := range []string{CheckGlueJobStale, CheckGlueAllRunsFailed, CheckGlueLastRunFailed, CheckGlueFailedRunWaste, CheckGlueOversizedWorker} {
		if _, ok := got[id]; ok {
			t.Errorf("run-based check %s fired despite RunsKnown=false", id)
		}
	}
}

func TestGlueFailedRunWaste(t *testing.T) {
	j := healthyJob()
	// Two failed runs with real DPUSeconds → estimable waste over the floor.
	j.Runs = []GlueRun{
		{State: "FAILED", Started: time.Now(), DPUSeconds: 7416},
		{State: "FAILED", Started: time.Now().Add(-time.Hour), DPUSeconds: 7416},
		{State: "SUCCEEDED", Started: time.Now().Add(-2 * time.Hour), DPUSeconds: 7416},
	}
	f, ok := analyzeJob(j)[CheckGlueFailedRunWaste]
	if !ok {
		t.Fatal("expected GLU-COST-001")
	}
	if f.EstMonthlyUSD <= 0 {
		t.Errorf("waste finding should carry a dollar estimate, got %v", f.EstMonthlyUSD)
	}
}

func TestGlueOversizedWorkers(t *testing.T) {
	j := healthyJob()
	j.NumberOfWorkers = 20
	j.Runs = []GlueRun{{State: "SUCCEEDED", Started: time.Now(), ExecSecs: 30}}
	if _, ok := analyzeJob(j)[CheckGlueOversizedWorker]; !ok {
		t.Error("expected GLU-COST-002 for 20 workers and a 30s run")
	}
	// A long run with many workers is justified — no finding.
	j.Runs = []GlueRun{{State: "SUCCEEDED", Started: time.Now(), ExecSecs: 1800}}
	if _, ok := analyzeJob(j)[CheckGlueOversizedWorker]; ok {
		t.Error("long successful run should not flag GLU-COST-002")
	}
}

func TestGlueCrawlerChecks(t *testing.T) {
	snap := GlueSnapshot{Region: "us-east-1", Now: time.Now(), Crawlers: []GlueCrawler{
		{Name: "failed", State: "READY", LastCrawlStatus: "FAILED"},
		{Name: "stuck", State: "RUNNING", RunningElapsed: 8 * time.Hour},
		{Name: "ok-running", State: "RUNNING", RunningElapsed: 5 * time.Minute},
		{Name: "ok", State: "READY", LastCrawlStatus: "SUCCEEDED"},
	}}
	got := byID(AnalyzeGlue(snap))
	if f, ok := got[CheckGlueCrawlerFailed]; !ok || f.Resource != "failed" {
		t.Errorf("expected GLU-CRAWL-001 on 'failed', got %+v", f)
	}
	if f, ok := got[CheckGlueCrawlerStuck]; !ok || f.Resource != "stuck" {
		t.Errorf("expected GLU-CRAWL-002 on 'stuck', got %+v", f)
	}
}

func TestGlueConnectionMissingNetwork(t *testing.T) {
	base := GlueSnapshot{
		Region: "us-east-1", Now: time.Now(), NetworkRefsKnown: true,
		ExistingSubnets: map[string]bool{"subnet-live": true},
		ExistingSGs:     map[string]bool{"sg-live": true},
		Connections: []GlueConnection{
			{Name: "broken", SubnetID: "subnet-gone", SecurityGroupIDs: []string{"sg-live", "sg-gone"}},
			{Name: "healthy", SubnetID: "subnet-live", SecurityGroupIDs: []string{"sg-live"}},
			{Name: "no-vpc"},
		},
	}
	got := byID(AnalyzeGlue(base))
	f, ok := got[CheckGlueConnMissingNet]
	if !ok || f.Resource != "broken" {
		t.Fatalf("expected GLU-CONN-001 on 'broken', got %+v", f)
	}

	// EC2 inventory unknown → check stays silent even with a dangling ref.
	base.NetworkRefsKnown = false
	if _, ok := byID(AnalyzeGlue(base))[CheckGlueConnMissingNet]; ok {
		t.Error("GLU-CONN-001 should stay silent when NetworkRefsKnown is false")
	}
}

func TestGlueChecksRegistered(t *testing.T) {
	for _, id := range []string{
		CheckGlueAllRunsFailed, CheckGlueJobStale, CheckGlueLastRunFailed,
		CheckGlueCrawlerFailed, CheckGlueCrawlerStuck, CheckGlueFailedRunWaste,
		CheckGlueOversizedWorker, CheckGlueNoSecurityConf, CheckGlueConnMissingNet,
	} {
		if _, ok := CheckByID(id); !ok {
			t.Errorf("check %s is not registered in checks.go", id)
		}
	}
}
