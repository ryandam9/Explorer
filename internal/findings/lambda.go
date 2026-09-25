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
	CheckLambdaIdle               = "LAM-USE-001"
	CheckLambdaLogsNeverExpire    = "LAM-LOG-001"
	CheckLambdaPublicURL          = "LAM-SEC-001"
	CheckLambdaPublicPolicy       = "LAM-SEC-002"
	CheckLambdaArm64Candidate     = "LAM-COST-001"
)

// lambdaIdleWindow is the usage lookback behind LAM-USE-001 and LAM-COST-001.
const lambdaIdleWindowDays = 30

// logStoragePerGBMonth is CloudWatch Logs' standard storage price (us-east-1;
// most regions match), for sizing what a never-expiring log group costs.
const logStoragePerGBMonth = 0.03

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

	Architectures []string
	HasLayers     bool
	LastModified  time.Time

	// Usage over the last 30 days (AWS/Lambda Invocations, all versions).
	// UsageKnown is false when the metric was not read — denied, failed, or
	// simply not collected (audit reads only ListFunctions) — which silences
	// the idle and arm64 checks.
	UsageKnown     bool
	Invocations30d float64

	// The function's log group. LogGroupKnown is false when it was not read;
	// LogGroupExists is false when the function has never logged.
	LogGroupKnown  bool
	LogGroupExists bool
	LogGroup       string
	RetentionDays  int32 // 0 = never expire
	StoredBytes    int64

	// Who can invoke it: the resource policy (lambda:GetPolicy; "" = none) and
	// the function URLs' auth types. *Known false = not read.
	PolicyKnown  bool
	Policy       string
	URLKnown     bool
	URLAuthTypes []string
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
		checkLambdaIdle(snap, f, &out)
		checkLambdaLogRetention(snap, f, &out)
		checkLambdaPublicURL(snap, f, &out)
		checkLambdaPublicPolicy(snap, f, &out)
		checkLambdaArm64(snap, f, &out)
	}
	return out
}

// checkLambdaIdle flags a function with no invocation in the last 30 days.
// Informational: a monthly or on-demand job is legitimately quiet, so it asks
// to confirm rather than asserting the function is dead.
func checkLambdaIdle(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.UsageKnown || f.Invocations30d > 0 {
		return
	}
	detail := fmt.Sprintf("No invocations in the last %d days (AWS/Lambda Invocations, all versions and aliases).", lambdaIdleWindowDays)
	if !f.LastModified.IsZero() {
		detail += fmt.Sprintf(" Last modified %s.", f.LastModified.Format("2006-01-02"))
	}
	*out = append(*out, Finding{
		ID: CheckLambdaIdle, Severity: SevInfo, Service: "lambda", Region: snap.Region,
		Resource: f.Name, ARN: f.ARN,
		Title:  "Lambda function not invoked in 30 days",
		Detail: detail,
		Fix:    "Confirm nothing still depends on it (check its triggers and schedules), then delete it — or tag it as intentionally dormant.",
	})
}

// checkLambdaLogRetention flags a function whose log group never expires, sized
// by what it stores.
func checkLambdaLogRetention(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.LogGroupKnown || !f.LogGroupExists || f.RetentionDays > 0 {
		return
	}
	gb := float64(f.StoredBytes) / (1 << 30)
	*out = append(*out, Finding{
		ID: CheckLambdaLogsNeverExpire, Severity: SevInfo, Service: "lambda", Region: snap.Region,
		Resource: f.Name, ARN: f.ARN,
		Title: "Lambda function's log group never expires",
		Detail: fmt.Sprintf("Log group %s has no retention policy, so its logs are kept (and billed) forever; it stores %s (≈ $%.2f/month at $%.2f per GB-month).",
			f.LogGroup, formatBytes(f.StoredBytes), gb*logStoragePerGBMonth, logStoragePerGBMonth),
		Fix: fmt.Sprintf("aws logs put-retention-policy --log-group-name %s --retention-in-days 30", f.LogGroup),
	})
}

// checkLambdaPublicURL flags a function URL that needs no authentication.
// A warning, not critical: public webhooks are a legitimate design — but the
// function must then authenticate callers itself.
func checkLambdaPublicURL(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.URLKnown {
		return
	}
	for _, a := range f.URLAuthTypes {
		if strings.EqualFold(a, "NONE") {
			*out = append(*out, Finding{
				ID: CheckLambdaPublicURL, Severity: SevWarning, Service: "lambda", Region: snap.Region,
				Resource: f.Name, ARN: f.ARN,
				Title:  "Lambda function URL allows unauthenticated invocation",
				Detail: "A function URL with auth type NONE can be called by anyone on the internet; any authentication has to happen inside the function.",
				Fix:    "Switch the URL to AWS_IAM auth, or put it behind API Gateway / CloudFront with auth — unless it is a deliberate public webhook that verifies callers itself.",
			})
			return
		}
	}
}

// checkLambdaPublicPolicy flags a resource policy that lets any principal
// invoke the function with no condition scoping it (Security Hub Lambda.1).
// A grant scoped by SourceArn/SourceAccount/org, or one that exists only for a
// NONE-auth URL (covered by LAM-SEC-001), does not fire.
func checkLambdaPublicPolicy(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.PolicyKnown || strings.TrimSpace(f.Policy) == "" {
		return
	}
	grants, err := ParseLambdaPolicy(f.Policy)
	if err != nil {
		return // unreadable → under-warn
	}
	for _, g := range grants {
		if g.Unrestricted() {
			sid := g.Sid
			if sid == "" {
				sid = "(no Sid)"
			}
			*out = append(*out, Finding{
				ID: CheckLambdaPublicPolicy, Severity: SevCritical, Service: "lambda", Region: snap.Region,
				Resource: f.Name, ARN: f.ARN,
				Title:  "Lambda resource policy lets anyone invoke the function",
				Detail: fmt.Sprintf("Statement %s allows Principal \"*\" to invoke the function with no condition, so any AWS account can call it.", sid),
				Fix:    "Scope the statement with an AWS:SourceArn / AWS:SourceAccount (or aws:PrincipalOrgID) condition, or name the principal — then remove the open statement (aws lambda remove-permission).",
			})
			return
		}
	}
}

// arm64Runtimes are the managed runtimes whose code is architecture-neutral
// (interpreted or JIT), so moving to arm64 is a configuration change.
var arm64Runtimes = []string{"python", "nodejs", "java", "dotnet", "ruby"}

// checkLambdaArm64 suggests arm64 (Graviton) for an x86 function that is in
// use. It stays silent where the move may not be a config change: container
// images, custom runtimes (compiled binaries), and functions with layers
// (which may ship native x86 code).
func checkLambdaArm64(snap LambdaSnapshot, f LambdaFunction, out *[]Finding) {
	if !f.UsageKnown || f.Invocations30d == 0 || f.HasLayers || f.PackageType == "Image" {
		return
	}
	for _, a := range f.Architectures {
		if a == "arm64" {
			return
		}
	}
	neutral := false
	for _, p := range arm64Runtimes {
		if strings.HasPrefix(f.Runtime, p) {
			neutral = true
		}
	}
	if !neutral {
		return
	}
	*out = append(*out, Finding{
		ID: CheckLambdaArm64Candidate, Severity: SevInfo, Service: "lambda", Region: snap.Region,
		Resource: f.Name, ARN: f.ARN,
		Title: "Lambda function could run on arm64 (Graviton)",
		Detail: fmt.Sprintf("It runs %s on x86_64 and was invoked %.0f times in the last %d days. arm64 is about 20%% cheaper per GB-second and often as fast or faster.",
			f.Runtime, f.Invocations30d, lambdaIdleWindowDays),
		Fix: "Test on arm64 (native dependencies in the package must have arm64 builds), then update the function's architecture to arm64.",
	})
}

// formatBytes renders a byte count as B/KB/MB/GB.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
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
