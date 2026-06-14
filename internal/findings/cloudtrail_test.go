package findings

import "testing"

// a multi-region trail that is logging, with global events and clean hygiene —
// the configuration that should produce zero findings.
func healthyTrail() CTTrail {
	return CTTrail{
		Name:                       "org-trail",
		HomeRegion:                 "us-east-1",
		IsMultiRegion:              true,
		IncludeGlobalServiceEvents: true,
		LogFileValidationEnabled:   true,
		KMSKeyID:                   "arn:aws:kms:us-east-1:111122223333:key/abc",
		CloudWatchLogsGroupARN:     "arn:aws:logs:us-east-1:111122223333:log-group:ct",
		StatusKnown:                true,
		IsLogging:                  true,
		SelectorsKnown:             true,
		LogsAllManagementEvents:    true,
	}
}

func TestAnalyzeCloudTrail_HealthyTrailHasNoFindings(t *testing.T) {
	snap := CloudTrailSnapshot{TrailsKnown: true, Trails: []CTTrail{healthyTrail()}}
	if fs := AnalyzeCloudTrail(snap); len(fs) != 0 {
		t.Errorf("a healthy multi-region trail should produce no findings, got %+v", fs)
	}
}

func TestAnalyzeCloudTrail_NoTrailsFiresCritical(t *testing.T) {
	snap := CloudTrailSnapshot{TrailsKnown: true}
	got := ids(AnalyzeCloudTrail(snap))
	if got[CheckTrailNotLogging] != 1 {
		t.Errorf("no trails should fire %s once, got %d", CheckTrailNotLogging, got[CheckTrailNotLogging])
	}
}

func TestAnalyzeCloudTrail_TrailsUnknownIsSilent(t *testing.T) {
	// DescribeTrails was denied: TrailsKnown=false. The coverage check must not
	// raise a false CRITICAL off data it never saw.
	snap := CloudTrailSnapshot{TrailsKnown: false}
	if fs := AnalyzeCloudTrail(snap); len(fs) != 0 {
		t.Errorf("unknown trail listing must stay silent, got %+v", fs)
	}
}

func TestAnalyzeCloudTrail_SingleRegionTrailIsNotCoverage(t *testing.T) {
	// A logging single-region trail does not satisfy the multi-region coverage
	// requirement, so the cornerstone check still fires.
	tr := healthyTrail()
	tr.IsMultiRegion = false
	snap := CloudTrailSnapshot{TrailsKnown: true, Trails: []CTTrail{tr}}
	got := ids(AnalyzeCloudTrail(snap))
	if got[CheckTrailNotLogging] != 1 {
		t.Errorf("single-region-only coverage should fire %s, got %d", CheckTrailNotLogging, got[CheckTrailNotLogging])
	}
}

func TestAnalyzeCloudTrail_HygieneChecks(t *testing.T) {
	tr := healthyTrail()
	tr.LogFileValidationEnabled = false
	tr.KMSKeyID = ""
	tr.CloudWatchLogsGroupARN = ""
	tr.LogsAllManagementEvents = false
	snap := CloudTrailSnapshot{TrailsKnown: true, Trails: []CTTrail{tr}}

	got := ids(AnalyzeCloudTrail(snap))
	for _, id := range []string{
		CheckTrailLogValidationOff,
		CheckTrailNotKMSEncrypted,
		CheckTrailNoCloudWatchLogs,
		CheckTrailMgmtEventsPartial,
	} {
		if got[id] != 1 {
			t.Errorf("%s fired %d times, want 1", id, got[id])
		}
	}
	// The trail is still multi-region + logging + global, so coverage holds.
	if got[CheckTrailNotLogging] != 0 {
		t.Errorf("coverage should hold for a logging multi-region trail, %s fired %d", CheckTrailNotLogging, got[CheckTrailNotLogging])
	}
}

func TestAnalyzeCloudTrail_SelectorsUnknownSkipsMgmtCheck(t *testing.T) {
	// GetEventSelectors was denied: don't guess that coverage is partial.
	tr := healthyTrail()
	tr.SelectorsKnown = false
	tr.LogsAllManagementEvents = false
	snap := CloudTrailSnapshot{TrailsKnown: true, Trails: []CTTrail{tr}}
	if got := ids(AnalyzeCloudTrail(snap)); got[CheckTrailMgmtEventsPartial] != 0 {
		t.Errorf("unknown selectors must not fire %s, got %d", CheckTrailMgmtEventsPartial, got[CheckTrailMgmtEventsPartial])
	}
}
