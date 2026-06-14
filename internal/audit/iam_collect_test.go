package audit

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// A burst of identical "context deadline exceeded" errors — exactly what a
// large account produces when the IAM scan outlasts the per-family timeout
// (issue #154) — must collapse into a single actionable summary.
func TestDedupeIAMErrorsCollapsesTimeouts(t *testing.T) {
	var errs []model.ExploreError
	for i := 0; i < 40; i++ {
		errs = append(errs, model.ExploreError{
			Service: "iam", Region: "global", Code: "CollectionError",
			Message: "operation error IAM: GetRole, context deadline exceeded",
		})
	}

	out := dedupeIAMErrors(errs, 30*time.Second)
	if len(out) != 1 {
		t.Fatalf("expected 40 timeout errors to collapse to 1, got %d:\n%+v", len(out), out)
	}
	if out[0].Code != "Timeout" {
		t.Errorf("collapsed error should be a Timeout, got %q", out[0].Code)
	}
	if !strings.Contains(out[0].Message, "40 IAM API call") {
		t.Errorf("summary should count the skipped calls, got %q", out[0].Message)
	}
	if !strings.Contains(out[0].Message, "30s") {
		t.Errorf("summary should mention the timeout so the user can raise it, got %q", out[0].Message)
	}
}

// Distinct real errors survive; only exact duplicates are folded away, and
// timeouts still collapse into the single trailing summary.
func TestDedupeIAMErrorsKeepsDistinctAndDeduplicates(t *testing.T) {
	errs := []model.ExploreError{
		{Service: "iam", Region: "global", Code: "AccessDenied", Message: "not authorized to GetRole"},
		{Service: "iam", Region: "global", Code: "AccessDenied", Message: "not authorized to GetRole"}, // dup
		{Service: "iam", Region: "global", Code: "CollectionError", Message: "throttled"},
		{Service: "iam", Region: "global", Code: "CollectionError", Message: "context deadline exceeded"},
	}

	out := dedupeIAMErrors(errs, 30*time.Second)

	var access, throttle, timeout int
	for _, e := range out {
		switch {
		case e.Code == "AccessDenied":
			access++
		case e.Code == "Timeout":
			timeout++
		case strings.Contains(e.Message, "throttled"):
			throttle++
		}
	}
	if access != 1 {
		t.Errorf("duplicate AccessDenied should fold to 1, got %d", access)
	}
	if throttle != 1 {
		t.Errorf("distinct throttle error should be kept, got %d", throttle)
	}
	if timeout != 1 {
		t.Errorf("the lone deadline error should yield 1 timeout summary, got %d", timeout)
	}
}

// With no timeouts, the summary line is not added.
func TestDedupeIAMErrorsNoTimeoutNoSummary(t *testing.T) {
	errs := []model.ExploreError{
		{Service: "iam", Region: "global", Code: "AccessDenied", Message: "denied"},
	}
	out := dedupeIAMErrors(errs, 30*time.Second)
	if len(out) != 1 || out[0].Code != "AccessDenied" {
		t.Fatalf("expected the single real error untouched, got %+v", out)
	}
}

func TestIsDeadline(t *testing.T) {
	if !isDeadline(context.DeadlineExceeded) {
		t.Error("DeadlineExceeded should be detected")
	}
	if !isDeadline(context.Canceled) {
		t.Error("Canceled should be detected")
	}
	if !isDeadline(fmt.Errorf("operation error IAM: GetRole: %w", context.DeadlineExceeded)) {
		t.Error("wrapped DeadlineExceeded should be detected")
	}
	if isDeadline(fmt.Errorf("AccessDenied")) {
		t.Error("a plain error should not be a deadline")
	}
}

// recordErrs routes timed-out calls into a collapsible deadline entry rather
// than one classified error per call.
func TestRecordErrsFoldsDeadlines(t *testing.T) {
	rec := &errRecorder{region: "global"}
	errs := []error{
		context.DeadlineExceeded,
		context.DeadlineExceeded,
		fmt.Errorf("boom"),
		nil,
	}
	recordErrs(rec, errs)

	out := dedupeIAMErrors(rec.errs, time.Minute)
	var timeout, other int
	for _, e := range out {
		if e.Code == "Timeout" {
			timeout++
		} else {
			other++
		}
	}
	if timeout != 1 {
		t.Errorf("two deadline errors should collapse to 1 summary, got %d", timeout)
	}
	if other != 1 {
		t.Errorf("the real error should be kept, got %d", other)
	}
}
