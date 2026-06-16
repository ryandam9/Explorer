package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestExitError(t *testing.T) {
	e := &exitError{code: 2, msg: ""}
	if e.ExitCode() != 2 {
		t.Errorf("ExitCode() = %d, want 2", e.ExitCode())
	}
	if e.Error() != "" {
		t.Errorf("Error() = %q, want empty", e.Error())
	}

	// main maps the exit code by matching the ExitCode() interface through the
	// error chain, so a wrapped exitError must still resolve.
	var coder interface{ ExitCode() int }
	if !errors.As(error(e), &coder) {
		t.Fatal("exitError should satisfy the ExitCode interface")
	}
	if coder.ExitCode() != 2 {
		t.Errorf("resolved ExitCode = %d, want 2", coder.ExitCode())
	}
}

// TestAuditFailOnRejectsTUI verifies the --fail-on + --tui guard returns an
// error (instead of os.Exit), reachable now that the handler is returnable.
func TestAuditFailOnRejectsTUI(t *testing.T) {
	savedFailOn, savedTUI, savedOnly, savedIgnore := auditFailOn, auditTUI, auditOnly, auditIgnore
	t.Cleanup(func() {
		auditFailOn, auditTUI, auditOnly, auditIgnore = savedFailOn, savedTUI, savedOnly, savedIgnore
	})

	auditOnly, auditIgnore = nil, nil
	auditFailOn, auditTUI = "critical", true

	err := auditCmd.RunE(auditCmd, nil)
	if err == nil {
		t.Fatal("expected --fail-on with --tui to return an error")
	}
	if !strings.Contains(err.Error(), "--tui") {
		t.Errorf("error should explain the --tui conflict, got: %v", err)
	}
}

// TestSummaryBaselineDiffMutuallyExclusive verifies the --baseline/--diff guard
// returns an error rather than terminating the process.
func TestSummaryBaselineDiffMutuallyExclusive(t *testing.T) {
	savedBaseline, savedDiff := summaryBaseline, summaryDiff
	t.Cleanup(func() { summaryBaseline, summaryDiff = savedBaseline, savedDiff })

	summaryBaseline, summaryDiff = true, true
	err := summaryCmd.RunE(summaryCmd, nil)
	if err == nil {
		t.Fatal("expected --baseline and --diff together to return an error")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error should explain the conflict, got: %v", err)
	}
}
