package findings

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/costs"
)

// Glue (data integration) check IDs (stable; see README "The checks").
const (
	CheckGlueAllRunsFailed   = "GLU-JOB-001"
	CheckGlueJobStale        = "GLU-JOB-002"
	CheckGlueLastRunFailed   = "GLU-JOB-003"
	CheckGlueCrawlerFailed   = "GLU-CRAWL-001"
	CheckGlueCrawlerStuck    = "GLU-CRAWL-002"
	CheckGlueFailedRunWaste  = "GLU-COST-001"
	CheckGlueOversizedWorker = "GLU-COST-002"
	CheckGlueNoSecurityConf  = "GLU-SEC-001"
	CheckGlueConnMissingNet  = "GLU-CONN-001"
)

// Thresholds for the Glue checks. Tunable here; deliberately conservative so
// the linter under-warns rather than nags.
const (
	glueAllFailedMin    = 3 // need at least this many runs to call a streak
	glueAllFailedCap    = 5 // ...and check at most this many of the most recent
	glueStaleAge        = 30 * 24 * time.Hour
	glueStuckCrawlerAge = 6 * time.Hour
	glueOversizedWorker = 10  // workers
	glueShortRunSeconds = 120 // a "successful run far shorter than the fleet implies"
	glueWasteFloorUSD   = 1.0 // don't flag trivial failed-run spend
)

// GlueSnapshot is the per-region input to AnalyzeGlue.
type GlueSnapshot struct {
	Region      string
	Now         time.Time
	Jobs        []GlueJob
	Crawlers    []GlueCrawler
	Connections []GlueConnection

	// NetworkRefsKnown is true when the EC2 subnet/security-group inventory was
	// gathered successfully. The connection network check (GLU-CONN-001) needs
	// the complete set to call a reference "missing"; when EC2 describes are
	// denied it stays false and the check stays silent (under-warn).
	NetworkRefsKnown bool
	ExistingSubnets  map[string]bool
	ExistingSGs      map[string]bool
}

// GlueConnection is one connection's network posture, from its
// PhysicalConnectionRequirements.
type GlueConnection struct {
	Name             string
	ARN              string
	SubnetID         string
	SecurityGroupIDs []string
}

// GlueJob is one job's posture: its definition knobs the checks care about plus
// its recent run history (newest first).
type GlueJob struct {
	Name              string
	ARN               string
	HasSecurityConfig bool
	NumberOfWorkers   int32

	// RunsKnown is false when GetJobRuns was denied/failed — the run-based
	// checks then stay silent for this job (under-warn, never guess).
	RunsKnown bool
	Runs      []GlueRun // newest first
}

// GlueRun is one job run, reduced to the fields the checks need.
type GlueRun struct {
	State      string
	Started    time.Time
	ExecSecs   int32
	DPUSeconds float64
}

// GlueCrawler is one crawler's posture.
type GlueCrawler struct {
	Name            string
	ARN             string
	State           string // READY / RUNNING / STOPPING
	LastCrawlStatus string // SUCCEEDED / FAILED / CANCELLED
	RunningElapsed  time.Duration
}

// AnalyzeGlue runs every Glue health/cost check over the snapshot. Pure.
func AnalyzeGlue(snap GlueSnapshot) []Finding {
	var out []Finding
	for _, j := range snap.Jobs {
		checkGlueJob(snap, j, &out)
	}
	for _, c := range snap.Crawlers {
		checkGlueCrawler(snap, c, &out)
	}
	if snap.NetworkRefsKnown {
		for _, conn := range snap.Connections {
			checkGlueConnection(snap, conn, &out)
		}
	}
	return out
}

func checkGlueJob(snap GlueSnapshot, j GlueJob, out *[]Finding) {
	// Security configuration: encrypts CloudWatch logs, S3 outputs and job
	// bookmarks. Its absence is a posture gap independent of run history.
	if !j.HasSecurityConfig {
		*out = append(*out, Finding{
			ID: CheckGlueNoSecurityConf, Severity: SevWarning, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:  "Glue job has no security configuration",
			Detail: "Without a security configuration the job's CloudWatch logs, S3 outputs and bookmarks are not encrypted with a customer-managed key.",
			Fix:    "Attach a Glue security configuration (CloudWatch/S3/job-bookmark encryption) to the job.",
		})
	}

	if j.RunsKnown {
		checkGlueRunHistory(snap, j, out)
	}
}

func checkGlueRunHistory(snap GlueSnapshot, j GlueJob, out *[]Finding) {
	// Never run, or no run in the staleness window.
	if len(j.Runs) == 0 {
		*out = append(*out, Finding{
			ID: CheckGlueJobStale, Severity: SevInfo, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:  "Glue job has never run",
			Detail: "The job is defined but has no run history.",
			Fix:    "Run it, wire it into a trigger/workflow, or delete it if obsolete.",
		})
		return
	}
	if last := j.Runs[0]; !last.Started.IsZero() && snap.Now.Sub(last.Started) > glueStaleAge {
		days := int(snap.Now.Sub(last.Started).Hours() / 24)
		*out = append(*out, Finding{
			ID: CheckGlueJobStale, Severity: SevInfo, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:  "Glue job has not run recently",
			Detail: fmt.Sprintf("Last run was %d days ago.", days),
			Fix:    "Confirm the job is still needed; delete it if obsolete.",
		})
	}

	// Sustained failure streak vs a single latest failure: the streak is the
	// stronger, louder signal, so emit one or the other, not both.
	if streak := failedStreak(j.Runs); streak >= glueAllFailedMin {
		*out = append(*out, Finding{
			ID: CheckGlueAllRunsFailed, Severity: SevCritical, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:  "Glue job's recent runs are all failing",
			Detail: fmt.Sprintf("The last %d run(s) all ended in a failure state.", streak),
			Fix:    "Inspect the latest run's error/logs; the job has been broken for several runs.",
		})
	} else if isFailedRunState(j.Runs[0].State) {
		*out = append(*out, Finding{
			ID: CheckGlueLastRunFailed, Severity: SevWarning, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:  "Glue job's latest run failed",
			Detail: fmt.Sprintf("The most recent run ended in state %s.", j.Runs[0].State),
			Fix:    "Check the run's error message and CloudWatch logs.",
		})
	}

	// Wasted spend on failed runs over the observed window.
	if waste, n := failedRunWaste(j.Runs); n > 0 && waste >= glueWasteFloorUSD {
		*out = append(*out, Finding{
			ID: CheckGlueFailedRunWaste, Severity: SevWarning, Service: "glue", Region: snap.Region,
			Resource: j.Name, ARN: j.ARN,
			Title:         "Glue job is burning DPU-hours on failed runs",
			Detail:        fmt.Sprintf("≈$%.2f spent on %d failed run(s) in the recent run history (estimate).", waste, n),
			Fix:           "Fix the failure so the spend produces output, or stop scheduling the job.",
			EstMonthlyUSD: waste,
		})
	}

	// Oversized worker allocation relative to a fast successful run.
	if j.NumberOfWorkers >= glueOversizedWorker {
		if r, ok := latestSuccessfulRun(j.Runs); ok && r.ExecSecs > 0 && r.ExecSecs < glueShortRunSeconds {
			*out = append(*out, Finding{
				ID: CheckGlueOversizedWorker, Severity: SevInfo, Service: "glue", Region: snap.Region,
				Resource: j.Name, ARN: j.ARN,
				Title:  "Glue job may be over-provisioned",
				Detail: fmt.Sprintf("Allocated %d workers but its last successful run took only %ds.", j.NumberOfWorkers, r.ExecSecs),
				Fix:    "Reduce NumberOfWorkers (or use auto-scaling / FLEX) to cut DPU-hours.",
			})
		}
	}
}

func checkGlueCrawler(snap GlueSnapshot, c GlueCrawler, out *[]Finding) {
	if strings.EqualFold(c.LastCrawlStatus, "FAILED") {
		*out = append(*out, Finding{
			ID: CheckGlueCrawlerFailed, Severity: SevWarning, Service: "glue", Region: snap.Region,
			Resource: c.Name, ARN: c.ARN,
			Title:  "Glue crawler's last crawl failed",
			Detail: "The most recent crawl ended in FAILED — the Data Catalog may be stale.",
			Fix:    "Check the crawler's CloudWatch logs and its data-store/IAM configuration.",
		})
	}
	if strings.EqualFold(c.State, "RUNNING") && c.RunningElapsed > glueStuckCrawlerAge {
		*out = append(*out, Finding{
			ID: CheckGlueCrawlerStuck, Severity: SevWarning, Service: "glue", Region: snap.Region,
			Resource: c.Name, ARN: c.ARN,
			Title:  "Glue crawler has been running unusually long",
			Detail: fmt.Sprintf("The crawler has been RUNNING for %s — it may be stuck.", c.RunningElapsed.Round(time.Minute)),
			Fix:    "Check progress; stop and investigate if it has hung.",
		})
	}
}

// checkGlueConnection flags a connection whose VPC network references no longer
// resolve: a deleted subnet or security group leaves jobs using the connection
// unable to start, with a confusing error at run time rather than at edit time.
func checkGlueConnection(snap GlueSnapshot, c GlueConnection, out *[]Finding) {
	var missing []string
	if c.SubnetID != "" && !snap.ExistingSubnets[c.SubnetID] {
		missing = append(missing, "subnet "+c.SubnetID)
	}
	for _, sg := range c.SecurityGroupIDs {
		if sg != "" && !snap.ExistingSGs[sg] {
			missing = append(missing, "security group "+sg)
		}
	}
	if len(missing) == 0 {
		return
	}
	*out = append(*out, Finding{
		ID: CheckGlueConnMissingNet, Severity: SevInfo, Service: "glue", Region: snap.Region,
		Resource: c.Name, ARN: c.ARN,
		Title:  "Glue connection references a missing subnet/security group",
		Detail: "The connection's network configuration points at " + strings.Join(missing, ", ") + ", which no longer exist — jobs using it will fail to start.",
		Fix:    "Update the connection's PhysicalConnectionRequirements to an existing subnet and security group, or delete the connection.",
	})
}

// --- pure helpers (fixture-tested) -----------------------------------------

func isFailedRunState(state string) bool {
	switch strings.ToUpper(state) {
	case "FAILED", "TIMEOUT", "ERROR":
		return true
	default:
		return false
	}
}

// failedStreak counts how many of the most recent runs (capped) all failed; it
// returns 0 unless every one of the first min(len,cap) runs failed and there
// are at least glueAllFailedMin of them.
func failedStreak(runs []GlueRun) int {
	n := len(runs)
	if n > glueAllFailedCap {
		n = glueAllFailedCap
	}
	if n < glueAllFailedMin {
		return 0
	}
	for i := 0; i < n; i++ {
		if !isFailedRunState(runs[i].State) {
			return 0
		}
	}
	return n
}

// failedRunWaste sums the estimated cost of failed runs and how many there were.
func failedRunWaste(runs []GlueRun) (usd float64, count int) {
	for _, r := range runs {
		if isFailedRunState(r.State) && r.DPUSeconds > 0 {
			usd += costs.GlueRunCostUSD(r.DPUSeconds)
			count++
		}
	}
	return usd, count
}

func latestSuccessfulRun(runs []GlueRun) (GlueRun, bool) {
	for _, r := range runs {
		if strings.EqualFold(r.State, "SUCCEEDED") {
			return r, true
		}
	}
	return GlueRun{}, false
}
