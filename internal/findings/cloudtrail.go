package findings

import (
	"fmt"
	"time"
)

// CloudTrail configuration check IDs (stable; see README "The checks"). These
// audit the account's audit trail itself: is API activity being recorded, and
// are the trails configured so the record is trustworthy and useful.
const (
	CheckTrailNotLogging        = "CT-TRAIL-001"
	CheckTrailLogValidationOff  = "CT-TRAIL-002"
	CheckTrailNotKMSEncrypted   = "CT-TRAIL-003"
	CheckTrailNoCloudWatchLogs  = "CT-TRAIL-004"
	CheckTrailMgmtEventsPartial = "CT-TRAIL-005"
)

// CloudTrailSnapshot is the account-global input to AnalyzeCloudTrail. Trails
// are enumerated once (CloudTrail config is account-wide; multi-region trails
// surface from any region), so unlike the per-region snapshots there is no
// Region field — findings are labeled "global".
type CloudTrailSnapshot struct {
	Now time.Time

	// TrailsKnown is true when DescribeTrails succeeded. The "no trail
	// logging" check (CT-TRAIL-001) only fires when the listing is trusted:
	// raising a CRITICAL off a denied DescribeTrails would be a false alarm.
	TrailsKnown bool
	Trails      []CTTrail
}

// CTTrail is one trail's configuration and live status. The *Known flags mark
// which follow-up calls (GetTrailStatus, GetEventSelectors) succeeded, so a
// check stays silent (under-warns) rather than guessing from missing data.
type CTTrail struct {
	Name                       string
	HomeRegion                 string
	IsMultiRegion              bool
	IsOrganizationTrail        bool
	IncludeGlobalServiceEvents bool
	LogFileValidationEnabled   bool
	KMSKeyID                   string // empty = SSE-S3 only, no customer KMS key
	CloudWatchLogsGroupARN     string // empty = not delivering to CloudWatch Logs

	StatusKnown bool
	IsLogging   bool

	SelectorsKnown          bool
	LogsAllManagementEvents bool
}

// AnalyzeCloudTrail runs every CloudTrail configuration check over the
// snapshot. Pure: no AWS calls, fixture-testable.
func AnalyzeCloudTrail(snap CloudTrailSnapshot) []Finding {
	var out []Finding
	checkTrailCoverage(snap, &out)
	checkTrailHygiene(snap, &out)
	return out
}

// checkTrailCoverage is the cornerstone CT check: the account must have at
// least one multi-region trail that is actively logging and capturing global
// service events, or there is effectively no audit trail to investigate an
// incident with.
func checkTrailCoverage(snap CloudTrailSnapshot, out *[]Finding) {
	if !snap.TrailsKnown {
		return // couldn't list trails — under-warn rather than false-alarm
	}
	for _, t := range snap.Trails {
		if t.IsMultiRegion && t.StatusKnown && t.IsLogging && t.IncludeGlobalServiceEvents {
			return // covered
		}
	}
	*out = append(*out, Finding{
		ID: CheckTrailNotLogging, Severity: SevCritical, Service: "cloudtrail", Region: "global",
		Resource: "-",
		Title:    "No multi-region CloudTrail is actively logging",
		Detail: "No multi-region trail is logging with global service events included, " +
			"so API activity in this account is not being recorded — there is no audit trail for incident investigation.",
		Fix: "Create a multi-region trail with log file validation and start logging: " +
			"aws cloudtrail create-trail --name org-trail --is-multi-region-trail --enable-log-file-validation",
	})
}

// checkTrailHygiene runs the per-trail posture checks: tamper detection,
// encryption, CloudWatch Logs delivery and management-event coverage.
func checkTrailHygiene(snap CloudTrailSnapshot, out *[]Finding) {
	for _, t := range snap.Trails {
		scope := "home " + t.HomeRegion
		if t.IsMultiRegion {
			scope = "multi-region, home " + t.HomeRegion
		}

		if !t.LogFileValidationEnabled {
			*out = append(*out, Finding{
				ID: CheckTrailLogValidationOff, Severity: SevWarning, Service: "cloudtrail", Region: "global",
				Resource: t.Name,
				Title:    "Trail log file validation is disabled",
				Detail: fmt.Sprintf("Trail %q (%s) has no log file validation, so tampering with delivered log files cannot be detected.",
					t.Name, scope),
				Fix: "aws cloudtrail update-trail --name " + t.Name + " --enable-log-file-validation",
			})
		}

		if t.KMSKeyID == "" {
			*out = append(*out, Finding{
				ID: CheckTrailNotKMSEncrypted, Severity: SevWarning, Service: "cloudtrail", Region: "global",
				Resource: t.Name,
				Title:    "Trail logs are not encrypted with a KMS key",
				Detail: fmt.Sprintf("Trail %q (%s) delivers logs with SSE-S3 only; a customer-managed KMS key (SSE-KMS) adds access control and an audit trail over the logs themselves.",
					t.Name, scope),
				Fix: "aws cloudtrail update-trail --name " + t.Name + " --kms-key-id <key-arn>",
			})
		}

		if t.CloudWatchLogsGroupARN == "" {
			*out = append(*out, Finding{
				ID: CheckTrailNoCloudWatchLogs, Severity: SevInfo, Service: "cloudtrail", Region: "global",
				Resource: t.Name,
				Title:    "Trail is not delivering to CloudWatch Logs",
				Detail: fmt.Sprintf("Trail %q (%s) has no CloudWatch Logs group, so metric filters and alarms (root login, unauthorized API calls, …) cannot be built on its events.",
					t.Name, scope),
				Fix: "aws cloudtrail update-trail --name " + t.Name + " --cloud-watch-logs-log-group-arn <arn> --cloud-watch-logs-role-arn <arn>",
			})
		}

		if t.SelectorsKnown && !t.LogsAllManagementEvents {
			*out = append(*out, Finding{
				ID: CheckTrailMgmtEventsPartial, Severity: SevWarning, Service: "cloudtrail", Region: "global",
				Resource: t.Name,
				Title:    "Trail does not capture all management events",
				Detail: fmt.Sprintf("Trail %q (%s) is not recording all management (read and write) events, leaving gaps in the audit record.",
					t.Name, scope),
				Fix: "aws cloudtrail put-event-selectors --trail-name " + t.Name +
					` --event-selectors '[{"ReadWriteType":"All","IncludeManagementEvents":true}]'`,
			})
		}
	}
}
