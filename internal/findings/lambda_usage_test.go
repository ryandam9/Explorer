package findings

import (
	"strings"
	"testing"
	"time"
)

func lambdaIDs(fs []Finding) map[string]bool {
	out := map[string]bool{}
	for _, f := range fs {
		out[f.ID] = true
	}
	return out
}

func TestLambdaUsageAndAccessChecks(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	base := LambdaFunction{Name: "f", ARN: "arn:f", Runtime: "python3.12", PackageType: "Zip", HasDLQ: true,
		Architectures: []string{"x86_64"}, LogGroup: "/aws/lambda/f"}
	run := func(f LambdaFunction) map[string]bool {
		return lambdaIDs(AnalyzeLambda(LambdaSnapshot{Region: "r", Now: now, Functions: []LambdaFunction{f}}))
	}

	// Nothing known beyond the list: none of the new checks fire (audit's case).
	if got := run(base); got[CheckLambdaIdle] || got[CheckLambdaLogsNeverExpire] || got[CheckLambdaPublicURL] ||
		got[CheckLambdaPublicPolicy] || got[CheckLambdaArm64Candidate] {
		t.Errorf("unknown facts must silence the checks: %v", got)
	}

	idle := base
	idle.UsageKnown = true
	if got := run(idle); !got[CheckLambdaIdle] || got[CheckLambdaArm64Candidate] {
		t.Errorf("0 invocations → idle, and no arm64 advice for an unused function: %v", got)
	}

	busy := base
	busy.UsageKnown, busy.Invocations30d = true, 90
	if got := run(busy); got[CheckLambdaIdle] || !got[CheckLambdaArm64Candidate] {
		t.Errorf("a used x86 python function → arm64 candidate: %v", got)
	}
	for _, f := range []LambdaFunction{
		func() LambdaFunction { f := busy; f.Architectures = []string{"arm64"}; return f }(),
		func() LambdaFunction { f := busy; f.HasLayers = true; return f }(),
		func() LambdaFunction { f := busy; f.Runtime = "provided.al2023"; return f }(),
		func() LambdaFunction { f := busy; f.PackageType, f.Runtime = "Image", ""; return f }(),
	} {
		if run(f)[CheckLambdaArm64Candidate] {
			t.Errorf("arm64 advice should stay silent for %+v", f)
		}
	}

	logs := base
	logs.LogGroupKnown, logs.LogGroupExists, logs.StoredBytes = true, true, 3<<30
	fs := AnalyzeLambda(LambdaSnapshot{Region: "r", Now: now, Functions: []LambdaFunction{logs}})
	var logF *Finding
	for i := range fs {
		if fs[i].ID == CheckLambdaLogsNeverExpire {
			logF = &fs[i]
		}
	}
	if logF == nil || !strings.Contains(logF.Detail, "3.0 GB") || !strings.Contains(logF.Detail, "$0.09/month") ||
		!strings.Contains(logF.Fix, "put-retention-policy --log-group-name /aws/lambda/f") {
		t.Fatalf("never-expiring log group finding = %+v", logF)
	}
	for _, f := range []LambdaFunction{
		func() LambdaFunction { f := logs; f.RetentionDays = 30; return f }(),
		func() LambdaFunction { f := logs; f.LogGroupExists = false; return f }(),
	} {
		if run(f)[CheckLambdaLogsNeverExpire] {
			t.Errorf("retention set, or no log group: silent (%+v)", f)
		}
	}

	url := base
	url.URLKnown, url.URLAuthTypes = true, []string{"AWS_IAM", "NONE"}
	if !run(url)[CheckLambdaPublicURL] {
		t.Error("a NONE-auth URL should fire")
	}
	url.URLAuthTypes = []string{"AWS_IAM"}
	if run(url)[CheckLambdaPublicURL] {
		t.Error("an IAM-auth URL is not public")
	}

	pol := base
	pol.PolicyKnown = true
	pol.Policy = `{"Statement":[{"Sid":"open","Effect":"Allow","Principal":"*","Action":"lambda:InvokeFunction"}]}`
	fs = AnalyzeLambda(LambdaSnapshot{Region: "r", Now: now, Functions: []LambdaFunction{pol}})
	var polF *Finding
	for i := range fs {
		if fs[i].ID == CheckLambdaPublicPolicy {
			polF = &fs[i]
		}
	}
	if polF == nil || polF.Severity != SevCritical || !strings.Contains(polF.Detail, "Statement open") {
		t.Fatalf("open policy finding = %+v", polF)
	}
	pol.Policy = `{"Statement":[{"Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"lambda:InvokeFunction","Condition":{"ArnLike":{"AWS:SourceArn":"arn:aws:s3:::b"}}},
		{"Effect":"Allow","Principal":"*","Action":"lambda:InvokeFunctionUrl","Condition":{"StringEquals":{"lambda:FunctionUrlAuthType":"NONE"}}}]}`
	if run(pol)[CheckLambdaPublicPolicy] {
		t.Error("scoped grants (and the URL grant, covered by LAM-SEC-001) must not fire")
	}
}
