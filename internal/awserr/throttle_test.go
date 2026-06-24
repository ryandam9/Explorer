package awserr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsThrottle_Codes(t *testing.T) {
	codes := []string{
		"Throttling",
		"ThrottlingException",
		"TooManyRequestsException",
		"RequestLimitExceeded",
		"ProvisionedThroughputExceededException",
		"SlowDown",
	}
	for _, code := range codes {
		err := &mockAPIError{code: code, message: "slow down"}
		if !IsThrottle(err) {
			t.Errorf("expected true for throttle code %q", code)
		}
	}
}

func TestIsThrottle_PhraseInWrappedError(t *testing.T) {
	// The shape the SDK produces after exhausting retries — the code is buried,
	// but the "Rate exceeded" phrase remains in the chain.
	inner := &mockAPIError{code: "", message: "api error ThrottlingException: Rate exceeded"}
	wrapped := fmt.Errorf("operation error CloudWatch Logs: DescribeSubscriptionFilters, exceeded maximum number of attempts, 3: %w", inner)
	if !IsThrottle(wrapped) {
		t.Error("expected true for wrapped Rate exceeded error")
	}
}

func TestIsThrottle_Negative(t *testing.T) {
	cases := []error{
		nil,
		errors.New("some unrelated failure"),
		&mockAPIError{code: "AccessDenied", message: "denied"},
		&mockAPIError{code: "ValidationException", message: "bad input"},
	}
	for _, err := range cases {
		if IsThrottle(err) {
			t.Errorf("expected false for %v", err)
		}
	}
}

// A whole storm of throttles with distinct RequestIDs must classify to one
// identical (code, message), so the recorder's dedup collapses them.
func TestClassify_ThrottleIsStableAcrossRequestIDs(t *testing.T) {
	mk := func(reqID string) error {
		return errors.New("operation error CloudWatch Logs: DescribeSubscriptionFilters, " +
			"exceeded maximum number of attempts, 3, https response error StatusCode: 400, " +
			"RequestID: " + reqID + ", api error ThrottlingException: Rate exceeded")
	}
	code1, msg1 := Classify(mk("aaa-111"), "logs", "")
	code2, msg2 := Classify(mk("bbb-222"), "logs", "")
	if code1 != "Throttling" {
		t.Fatalf("expected code Throttling, got %q", code1)
	}
	if code1 != code2 || msg1 != msg2 {
		t.Errorf("throttle classification must be stable across RequestIDs:\n  %q / %q\n  %q / %q", code1, msg1, code2, msg2)
	}
	if strings.Contains(msg1, "RequestID") || strings.Contains(msg1, "aaa-111") {
		t.Errorf("throttle message must not embed the RequestID, got %q", msg1)
	}
	if !strings.Contains(msg1, "logs") {
		t.Errorf("throttle message should name the service, got %q", msg1)
	}
}
