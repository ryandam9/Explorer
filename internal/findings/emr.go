package findings

import (
	"fmt"
	"strings"
	"time"
)

// Amazon EMR check IDs (stable; see README "The checks").
const (
	CheckEMRIdleCluster      = "EMR-COST-001"
	CheckEMRLongRunning      = "EMR-COST-002"
	CheckEMRLatestStepFail   = "EMR-STEP-001"
	CheckEMRTerminatedErr    = "EMR-STEP-002"
	CheckEMRNoLogURI         = "EMR-LOG-001"
	CheckEMRNoSecurityConf   = "EMR-SEC-001"
	CheckEMRFSConsistentView = "EMR-EMRFS-001"
)

// Thresholds for the EMR checks. Tunable here; deliberately conservative so the
// linter under-warns rather than nags.
const (
	emrIdleAge    = 24 * time.Hour     // WAITING this long with nothing to do is wasteful
	emrLongRunAge = 7 * 24 * time.Hour // a non-auto-terminating cluster up this long
)

// EMRSnapshot is the per-region input to AnalyzeEMR.
type EMRSnapshot struct {
	Region   string
	Now      time.Time
	Clusters []EMRCluster
}

// EMRCluster is one cluster's posture, reduced to the fields the checks need.
type EMRCluster struct {
	ID                string
	Name              string
	ARN               string
	State             string
	Created           time.Time
	AutoTerminate     bool
	HasLogURI         bool
	HasSecurityConfig bool

	// StepsKnown is false when ListSteps was denied/failed — the step check
	// then stays silent for this cluster (under-warn, never guess).
	StepsKnown      bool
	LatestStepState string // newest step's state, "" when none

	// EMRFS S3-connector posture, derived from the release label + configurations
	// (see DeriveS3Connector). ConnectorKnown is false when DescribeCluster was
	// denied, so the connector-based checks stay silent (§8).
	ConnectorKnown      bool
	ConsistentView      bool   // EMRFS Consistent View enabled
	ConsistentViewTable string // its DynamoDB metadata table
}

// AnalyzeEMR runs every EMR health/cost check over the snapshot. Pure.
func AnalyzeEMR(snap EMRSnapshot) []Finding {
	var out []Finding
	for _, c := range snap.Clusters {
		checkEMRCluster(snap, c, &out)
	}
	return out
}

func checkEMRCluster(snap EMRSnapshot, c EMRCluster, out *[]Finding) {
	res := c.Name
	if res == "" {
		res = c.ID
	}

	// A cluster that died with errors is the loudest signal; it overrides the
	// live-cluster posture checks below (which don't apply to a dead cluster).
	if isTerminatedWithErrors(c.State) {
		*out = append(*out, Finding{
			ID: CheckEMRTerminatedErr, Severity: SevCritical, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "EMR cluster terminated with errors",
			Detail: "The cluster ended in TERMINATED_WITH_ERRORS — a bootstrap, step or hardware failure took it down.",
			Fix:    "Review the cluster's last step and the controller/stderr logs to find the root cause.",
		})
		return
	}

	// The remaining checks only apply to live clusters; a cleanly TERMINATED
	// cluster is gone and not worth flagging.
	if isTerminatedState(c.State) {
		return
	}

	// Idle: WAITING (ready, no work) past the idle threshold burns money.
	if strings.EqualFold(c.State, "WAITING") && !c.Created.IsZero() && snap.Now.Sub(c.Created) > emrIdleAge {
		hours := int(snap.Now.Sub(c.Created).Hours())
		*out = append(*out, Finding{
			ID: CheckEMRIdleCluster, Severity: SevWarning, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "EMR cluster idle (WAITING) for a long time",
			Detail: fmt.Sprintf("The cluster has been up ~%dh in WAITING — provisioned but doing no work.", hours),
			Fix:    "Submit work, enable an auto-termination policy, or terminate the cluster.",
		})
	}

	// Long-running cluster that won't auto-terminate.
	if !c.AutoTerminate && !c.Created.IsZero() && snap.Now.Sub(c.Created) > emrLongRunAge {
		days := int(snap.Now.Sub(c.Created).Hours() / 24)
		*out = append(*out, Finding{
			ID: CheckEMRLongRunning, Severity: SevInfo, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "Long-running EMR cluster without auto-termination",
			Detail: fmt.Sprintf("The cluster has been up ~%d days and has no auto-termination policy.", days),
			Fix:    "Confirm it should be persistent; otherwise set an auto-termination policy.",
		})
	}

	// No log destination: nobody can debug a failure after the fact.
	if !c.HasLogURI {
		*out = append(*out, Finding{
			ID: CheckEMRNoLogURI, Severity: SevWarning, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "EMR cluster has no log destination",
			Detail: "The cluster has no S3 log URI, so step/container/daemon logs are lost when nodes terminate.",
			Fix:    "Recreate the cluster with a LogUri (s3://…) so logs are archived.",
		})
	}

	// No security configuration: at-rest/in-transit encryption not enforced.
	if !c.HasSecurityConfig {
		*out = append(*out, Finding{
			ID: CheckEMRNoSecurityConf, Severity: SevWarning, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "EMR cluster has no security configuration",
			Detail: "Without a security configuration the cluster's at-rest and in-transit encryption are not enforced.",
			Fix:    "Attach an EMR security configuration (EBS/S3/local-disk and in-transit encryption).",
		})
	}

	// EMRFS Consistent View enabled (obsolete since S3 strong consistency).
	checkEMRFSConsistentView(snap, c, out)

	// Latest step failed.
	if c.StepsKnown && isFailedStepState(c.LatestStepState) {
		*out = append(*out, Finding{
			ID: CheckEMRLatestStepFail, Severity: SevWarning, Service: "emr", Region: snap.Region,
			Resource: res, ARN: c.ARN,
			Title:  "EMR cluster's latest step failed",
			Detail: fmt.Sprintf("The most recent step ended in state %s.", c.LatestStepState),
			Fix:    "Inspect the step's failure reason and its stderr log in S3.",
		})
	}
}

func isTerminatedWithErrors(state string) bool {
	return strings.EqualFold(state, "TERMINATED_WITH_ERRORS")
}

func isTerminatedState(state string) bool {
	switch strings.ToUpper(state) {
	case "TERMINATED", "TERMINATED_WITH_ERRORS", "TERMINATING":
		return true
	default:
		return false
	}
}

func isFailedStepState(state string) bool {
	switch strings.ToUpper(state) {
	case "FAILED", "CANCELLED", "INTERRUPTED":
		return true
	default:
		return false
	}
}
