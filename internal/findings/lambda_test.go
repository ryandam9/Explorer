package findings

import (
	"testing"
	"time"
)

// fixed reference date for the runtime-EOL fixtures:
//   - python3.9 deprecated 2025-12-15  → in the past   → LAM-RUN-001
//   - ruby3.2   deprecates  2026-03-31  → ~89 days out  → LAM-RUN-002
//   - python3.12 not in the EOL table  → silent
var lambdaNow = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func findingByID(fs []Finding, id string) (Finding, bool) {
	for _, f := range fs {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

func TestAnalyzeLambdaRuntimeDeprecated(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "legacy", ARN: "arn:legacy", Runtime: "python3.9", HasDLQ: true},
		},
	})
	f, ok := findingByID(out, CheckLambdaRuntimeDeprecated)
	if !ok {
		t.Fatalf("expected %s, got %+v", CheckLambdaRuntimeDeprecated, out)
	}
	if f.Severity != SevWarning || f.Resource != "legacy" {
		t.Errorf("unexpected finding: %+v", f)
	}
	if _, soon := findingByID(out, CheckLambdaRuntimeDeprecating); soon {
		t.Error("a past-deprecation runtime must not also fire the deprecating-soon check")
	}
}

func TestAnalyzeLambdaRuntimeDeprecatingSoon(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "soon", ARN: "arn:soon", Runtime: "ruby3.2", HasDLQ: true},
		},
	})
	f, ok := findingByID(out, CheckLambdaRuntimeDeprecating)
	if !ok {
		t.Fatalf("expected %s, got %+v", CheckLambdaRuntimeDeprecating, out)
	}
	if f.Severity != SevInfo {
		t.Errorf("deprecating-soon should be info, got %v", f.Severity)
	}
}

func TestAnalyzeLambdaSupportedRuntimeSilent(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			// Unknown/supported runtime + DLQ + active → no runtime/DLQ/health finding.
			{Name: "modern", ARN: "arn:modern", Runtime: "python3.12", HasDLQ: true, StateKnown: true, State: "Active", LastUpdateStatus: "Successful"},
		},
	})
	if len(out) != 0 {
		t.Errorf("expected no findings, got %+v", out)
	}
}

func TestAnalyzeLambdaContainerRuntimeSkipped(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "img", ARN: "arn:img", Runtime: "", PackageType: "Image", HasDLQ: true},
		},
	})
	if _, ok := findingByID(out, CheckLambdaRuntimeDeprecated); ok {
		t.Error("container-image function has no runtime; runtime check must be silent")
	}
}

func TestAnalyzeLambdaNoDLQ(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "nodlq", ARN: "arn:nodlq", Runtime: "python3.12", HasDLQ: false},
		},
	})
	f, ok := findingByID(out, CheckLambdaNoDLQ)
	if !ok {
		t.Fatalf("expected %s, got %+v", CheckLambdaNoDLQ, out)
	}
	if f.Severity != SevInfo {
		t.Errorf("no-DLQ should be info, got %v", f.Severity)
	}
}

func TestAnalyzeLambdaUnhealthy(t *testing.T) {
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "broken", ARN: "arn:broken", Runtime: "python3.12", HasDLQ: true, StateKnown: true, State: "Failed"},
			{Name: "stale", ARN: "arn:stale", Runtime: "python3.12", HasDLQ: true, StateKnown: true, State: "Active", LastUpdateStatus: "Failed"},
		},
	})
	var n int
	for _, f := range out {
		if f.ID == CheckLambdaUnhealthy {
			n++
			if f.Severity != SevWarning {
				t.Errorf("unhealthy should be warning, got %v", f.Severity)
			}
		}
	}
	if n != 2 {
		t.Errorf("expected 2 unhealthy findings, got %d (%+v)", n, out)
	}
}

func TestAnalyzeLambdaUnknownStateSilent(t *testing.T) {
	// StateKnown=false → the health check must stay silent (under-warn).
	out := AnalyzeLambda(LambdaSnapshot{
		Region: "us-east-1", Now: lambdaNow,
		Functions: []LambdaFunction{
			{Name: "sparse", ARN: "arn:sparse", Runtime: "python3.12", HasDLQ: true, StateKnown: false, State: ""},
		},
	})
	if _, ok := findingByID(out, CheckLambdaUnhealthy); ok {
		t.Error("unknown state must silence the health check")
	}
}
