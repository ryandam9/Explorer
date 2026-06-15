package findings

import "testing"

// Every check ID constant must be registered — the registry drives --ignore
// validation and SARIF rule metadata, so an unregistered check would be
// unsuppressible and rule-less.
func TestEveryCheckIDIsRegistered(t *testing.T) {
	ids := []string{
		CheckUnattachedVolume, CheckGP2Volume, CheckUnassociatedEIP,
		CheckIdleNATGateway, CheckLBNoHealthyTarget, CheckLBIdle,
		CheckStoppedWithEBS, CheckOldSnapshot, CheckUnusedAMI,
		CheckDDBOverProvision,
		CheckS3Public, CheckS3PABOff, CheckS3EncryptionOff,
		CheckEBSUnencrypted, CheckEBSDefaultEncOff, CheckPublicEBSSnapshot,
		CheckRDSPublic, CheckRDSUnencrypted, CheckPublicRDSSnapshot,
		CheckIMDSv1, CheckSGOpenPort, CheckLambdaURLNoAuth,
		CheckSQSOpenPolicy, CheckSNSOpenPolicy, CheckAlarmNoData,
		CheckOldAccessKey, CheckUnusedAccessKey, CheckUserNoMFA,
		CheckRootAccessKey, CheckUnusedRole, CheckWildcardPolicy,
		CheckUserAttachedPolicy, CheckOpenTrustPolicy,
		CheckQueueNoConsumers, CheckRedriveDangling, CheckDLQNotEmpty,
		CheckSubPending, CheckTopicNoSubs,
		CheckTrailNotLogging, CheckTrailLogValidationOff, CheckTrailNotKMSEncrypted,
		CheckTrailNoCloudWatchLogs, CheckTrailMgmtEventsPartial,
		CheckGlueAllRunsFailed, CheckGlueJobStale, CheckGlueLastRunFailed,
		CheckGlueCrawlerFailed, CheckGlueCrawlerStuck, CheckGlueFailedRunWaste,
		CheckGlueOversizedWorker, CheckGlueNoSecurityConf, CheckGlueConnMissingNet,
		CheckEMRIdleCluster, CheckEMRLongRunning, CheckEMRLatestStepFail,
		CheckEMRTerminatedErr, CheckEMRNoLogURI, CheckEMRNoSecurityConf,
	}
	if len(Checks()) != len(ids) {
		t.Errorf("registry has %d checks, constants list %d", len(Checks()), len(ids))
	}
	for _, id := range ids {
		meta, ok := CheckByID(id)
		if !ok {
			t.Errorf("check %s is not registered", id)
			continue
		}
		if meta.Name == "" || meta.Summary == "" {
			t.Errorf("check %s has incomplete metadata: %+v", id, meta)
		}
	}
}

func TestCheckByIDUnknown(t *testing.T) {
	if _, ok := CheckByID("NOPE-001"); ok {
		t.Error("unknown ID should not resolve")
	}
}

func TestParseSeverity(t *testing.T) {
	cases := map[string]Severity{
		"critical": SevCritical,
		"WARNING":  SevWarning,
		" info ":   SevInfo,
	}
	for in, want := range cases {
		got, err := ParseSeverity(in)
		if err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseSeverity("fatal"); err == nil {
		t.Error("unknown severity should error")
	}
}

func TestDrop(t *testing.T) {
	fs := []Finding{
		{ID: CheckUnattachedVolume, Resource: "a"},
		{ID: CheckGP2Volume, Resource: "b"},
		{ID: CheckUnattachedVolume, Resource: "c"},
	}
	got := Drop(fs, map[string]bool{CheckUnattachedVolume: true})
	if len(got) != 1 || got[0].Resource != "b" {
		t.Errorf("Drop = %+v", got)
	}
	if got := Drop(fs, nil); len(got) != 3 {
		t.Errorf("nil ignore should keep all, got %d", len(got))
	}
}

func TestAnyAtOrAbove(t *testing.T) {
	fs := []Finding{{Severity: SevInfo}, {Severity: SevWarning}}
	if !AnyAtOrAbove(fs, SevWarning) {
		t.Error("warning threshold should trip on a warning finding")
	}
	if AnyAtOrAbove(fs, SevCritical) {
		t.Error("critical threshold should not trip without criticals")
	}
	if !AnyAtOrAbove(fs, SevInfo) {
		t.Error("info threshold should trip on anything")
	}
	if AnyAtOrAbove(nil, SevInfo) {
		t.Error("no findings should never trip")
	}
}
