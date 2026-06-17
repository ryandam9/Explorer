package findings

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/expiry"
)

// Lambda (serverless compute) check IDs (stable; see README "The checks").
const (
	CheckLambdaRuntimeDeprecated  = "LAM-RUN-001"
	CheckLambdaRuntimeDeprecating = "LAM-RUN-002"
	CheckLambdaNoDLQ              = "LAM-CFG-001"
	CheckLambdaUnhealthy          = "LAM-CFG-002"
)

// lambdaDeprecatingSoon is how far ahead a runtime's deprecation date must be to
// fire the early-warning check rather than the past-deprecation one. Deliberately
// generous so a team has a full quarter's notice.
const lambdaDeprecatingSoon = 90 * 24 * time.Hour

// LambdaSnapshot is the per-region input to AnalyzeLambda.
type LambdaSnapshot struct {
	Region    string
	Now       time.Time
	Functions []LambdaFunction
}

// LambdaFunction is one function's posture, reduced to the fields the checks
// need. Every "known" flag follows the under-warn rule: when the list response
// omitted a fact, the dependent check stays silent rather than guessing.
type LambdaFunction struct {
	Name        string
	ARN         string
	Runtime     string // e.g. "python3.9"; empty for container-image (Image) functions
	PackageType string // "Zip" or "Image"

	// HasDLQ reports whether a dead-letter queue is configured (DeadLetterConfig
	// present). It is always derivable from ListFunctions, so it has no "known"
	// flag.
	HasDLQ bool

	// State / LastUpdateStatus drive the unhealthy check. StateKnown is false when
	// the list response carried no State string (so the check stays silent).
	StateKnown       bool
	State            string // ACTIVE / INACTIVE / PENDING / FAILED
	LastUpdateStatus string // Successful / Failed / InProgress
}

// AnalyzeLambda runs every Lambda health/EOL check over the snapshot. Pure — it
// reasons only over the snapshot and the static runtime-EOL table, never calling
// AWS — so each check is unit-testable with fixtures.
func AnalyzeLambda(snap LambdaSnapshot) []Finding {
	var out []Finding
	for _, f := range snap.Functions {
		checkLambdaRuntime(snap, f, &out)
		checkLambdaDLQ(snap, f, &out)
		checkLambdaHealth(snap, f, &out)
	}
	return out
}

// checkLambdaRuntime flags a function whose runtime has a published deprecation
// date: past dates (updates already blocked) fire the louder warning, dates
// within the lookahead window fire the early-warning info. Container-image
// functions carry no runtime identifier and are skipped.
func checkLambdaRuntime(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if f.Runtime == "" {
		return
	}
	date, known := expiry.LambdaRuntimeDeprecation(f.Runtime)
	if !known {
		return // unknown runtime → under-warn (no guess)
	}
	switch {
	case !date.After(snap.Now):
		*out = append(*out, Finding{
			ID: CheckLambdaRuntimeDeprecated, Severity: SevWarning, Service: "lambda", Region: snap.Region,
			Resource: f.Name, ARN: f.ARN,
			Title:  "Lambda function uses a deprecated runtime",
			Detail: fmt.Sprintf("Runtime %s was deprecated on %s; Lambda blocks updates to functions on a deprecated runtime.", f.Runtime, date.Format("2006-01-02")),
			Fix:    "Migrate the function to a supported runtime (test, then update the Runtime in the function configuration).",
		})
	case date.Sub(snap.Now) <= lambdaDeprecatingSoon:
		days := int(date.Sub(snap.Now).Hours() / 24)
		*out = append(*out, Finding{
			ID: CheckLambdaRuntimeDeprecating, Severity: SevInfo, Service: "lambda", Region: snap.Region,
			Resource: f.Name, ARN: f.ARN,
			Title:  "Lambda function's runtime is approaching deprecation",
			Detail: fmt.Sprintf("Runtime %s is scheduled for deprecation on %s (in %d days).", f.Runtime, date.Format("2006-01-02"), days),
			Fix:    "Plan a migration to a supported runtime before the deprecation date.",
		})
	}
}

// checkLambdaDLQ flags a function with no dead-letter queue. It is informational
// and worded honestly: an on-failure destination (which ListFunctions does not
// report) is an equally valid alternative, so the finding states what is known
// (no DLQ) without asserting failed events are definitely being dropped.
func checkLambdaDLQ(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if f.HasDLQ {
		return
	}
	*out = append(*out, Finding{
		ID: CheckLambdaNoDLQ, Severity: SevInfo, Service: "lambda", Region: snap.Region,
		Resource: f.Name, ARN: f.ARN,
		Title:  "Lambda function has no dead-letter queue",
		Detail: "No dead-letter queue is configured; failed asynchronous invocations are dropped unless an on-failure destination is set instead.",
		Fix:    "Configure a dead-letter queue (SQS/SNS) or an on-failure destination so failed async invocations are captured.",
	})
}

// checkLambdaHealth flags a function stuck in a failed state. It only fires when
// the list response actually reported a state (StateKnown), so a sparse response
// silences the check rather than guessing the function is healthy or broken.
func checkLambdaHealth(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.StateKnown {
		return
	}
	if strings.EqualFold(f.State, "Failed") || strings.EqualFold(f.LastUpdateStatus, "Failed") {
		detail := "The function is in a failed state and may not be invocable."
		if strings.EqualFold(f.LastUpdateStatus, "Failed") {
			detail = "The function's most recent update failed; it may be running stale code or be uninvocable."
		}
		*out = append(*out, Finding{
			ID: CheckLambdaUnhealthy, Severity: SevWarning, Service: "lambda", Region: snap.Region,
			Resource: f.Name, ARN: f.ARN,
			Title:  "Lambda function is in a failed state",
			Detail: detail,
			Fix:    "Check the function's StateReason / LastUpdateStatusReason and redeploy or fix the configuration.",
		})
	}
}
