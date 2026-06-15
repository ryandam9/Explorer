package findings

import (
	"testing"
	"time"
)

// has reports whether any finding has the given check ID.
func hasEMR(fs []Finding, id string) bool {
	for _, f := range fs {
		if f.ID == id {
			return true
		}
	}
	return false
}

func TestAnalyzeEMR_TerminatedWithErrors(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := EMRSnapshot{Region: "us-east-1", Now: now, Clusters: []EMRCluster{{
		ID: "j-1", Name: "dead", State: "TERMINATED_WITH_ERRORS",
		// These would otherwise fire, but a dead cluster only gets STEP-002.
		HasLogURI: false, HasSecurityConfig: false,
	}}}
	fs := AnalyzeEMR(snap)
	if !hasEMR(fs, CheckEMRTerminatedErr) {
		t.Error("expected EMR-STEP-002 for TERMINATED_WITH_ERRORS")
	}
	if hasEMR(fs, CheckEMRNoLogURI) || hasEMR(fs, CheckEMRNoSecurityConf) {
		t.Error("dead cluster should not get live-posture findings")
	}
}

func TestAnalyzeEMR_CleanlyTerminatedIsSilent(t *testing.T) {
	snap := EMRSnapshot{Region: "us-east-1", Now: time.Now().UTC(), Clusters: []EMRCluster{{
		ID: "j-2", Name: "gone", State: "TERMINATED", HasLogURI: false, HasSecurityConfig: false,
	}}}
	if fs := AnalyzeEMR(snap); len(fs) != 0 {
		t.Errorf("a cleanly terminated cluster should produce no findings, got %d", len(fs))
	}
}

func TestAnalyzeEMR_IdleAndLongRunning(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := EMRSnapshot{Region: "us-east-1", Now: now, Clusters: []EMRCluster{{
		ID: "j-3", Name: "idle", State: "WAITING",
		Created:       now.Add(-10 * 24 * time.Hour), // 10 days
		AutoTerminate: false,
		HasLogURI:     true, HasSecurityConfig: true,
	}}}
	fs := AnalyzeEMR(snap)
	if !hasEMR(fs, CheckEMRIdleCluster) {
		t.Error("expected EMR-COST-001 for a long-idle WAITING cluster")
	}
	if !hasEMR(fs, CheckEMRLongRunning) {
		t.Error("expected EMR-COST-002 for a >7d non-auto-terminating cluster")
	}
}

func TestAnalyzeEMR_HealthyRunningClusterIsQuiet(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := EMRSnapshot{Region: "us-east-1", Now: now, Clusters: []EMRCluster{{
		ID: "j-4", Name: "healthy", State: "RUNNING",
		Created:       now.Add(-1 * time.Hour),
		AutoTerminate: true,
		HasLogURI:     true, HasSecurityConfig: true,
		StepsKnown: true, LatestStepState: "COMPLETED",
	}}}
	if fs := AnalyzeEMR(snap); len(fs) != 0 {
		t.Errorf("a healthy cluster should produce no findings, got %d: %+v", len(fs), fs)
	}
}

func TestAnalyzeEMR_PostureAndStepChecks(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := EMRSnapshot{Region: "us-east-1", Now: now, Clusters: []EMRCluster{{
		ID: "j-5", Name: "leaky", State: "RUNNING",
		Created:       now.Add(-2 * time.Hour),
		AutoTerminate: true,
		HasLogURI:     false, HasSecurityConfig: false,
		StepsKnown: true, LatestStepState: "FAILED",
	}}}
	fs := AnalyzeEMR(snap)
	for _, id := range []string{CheckEMRNoLogURI, CheckEMRNoSecurityConf, CheckEMRLatestStepFail} {
		if !hasEMR(fs, id) {
			t.Errorf("expected %s", id)
		}
	}
	// Not idle (RUNNING), not long-running (2h, auto-terminate on).
	if hasEMR(fs, CheckEMRIdleCluster) || hasEMR(fs, CheckEMRLongRunning) {
		t.Error("did not expect cost findings on this cluster")
	}
}

func TestAnalyzeEMR_StepsUnknownStaysSilent(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	snap := EMRSnapshot{Region: "us-east-1", Now: now, Clusters: []EMRCluster{{
		ID: "j-6", Name: "denied-steps", State: "RUNNING",
		Created:       now.Add(-2 * time.Hour),
		AutoTerminate: true, HasLogURI: true, HasSecurityConfig: true,
		StepsKnown: false, LatestStepState: "FAILED", // unknown → must not fire
	}}}
	if hasEMR(AnalyzeEMR(snap), CheckEMRLatestStepFail) {
		t.Error("EMR-STEP-001 must stay silent when StepsKnown is false")
	}
}

func TestEMRChecksRegistered(t *testing.T) {
	for _, id := range []string{
		CheckEMRIdleCluster, CheckEMRLongRunning, CheckEMRLatestStepFail,
		CheckEMRTerminatedErr, CheckEMRNoLogURI, CheckEMRNoSecurityConf,
	} {
		if _, ok := CheckByID(id); !ok {
			t.Errorf("check %s not registered in checks.go", id)
		}
	}
}
