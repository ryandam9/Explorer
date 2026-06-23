package s3tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
)

// A denied s3:ListBuckets must surface as a named-privilege warning, never as a
// clean empty result (#364, CLAUDE.md §6a/§8: denied ≠ empty).
func TestListBucketsErrorStatus_AccessDeniedNamesPrivilege(t *testing.T) {
	for _, code := range accessDeniedCodes {
		err := &smithy.GenericAPIError{Code: code, Message: "nope"}
		got := listBucketsErrorStatus(err)
		if !strings.Contains(got, "s3:ListAllMyBuckets") {
			t.Errorf("%s: status %q does not name the required privilege", code, got)
		}
		if strings.Contains(strings.ToLower(got), "no accessible buckets") {
			t.Errorf("%s: a denial must not read like an empty list: %q", code, got)
		}
	}
}

// A non-denial API error is summarized, not mislabeled as a missing privilege.
func TestListBucketsErrorStatus_OtherErrorSummarized(t *testing.T) {
	err := &smithy.GenericAPIError{Code: "SlowDown", Message: "throttled"}
	got := listBucketsErrorStatus(err)
	if strings.Contains(got, "s3:ListAllMyBuckets") {
		t.Errorf("non-denial error should not mention the list privilege: %q", got)
	}
	if !strings.Contains(got, "SlowDown") {
		t.Errorf("status should summarize the underlying error: %q", got)
	}
}

func TestListBucketsErrorStatus_NonAPIError(t *testing.T) {
	got := listBucketsErrorStatus(errors.New("dial tcp: i/o timeout"))
	if !strings.Contains(got, "timeout") {
		t.Errorf("status should include the raw error text: %q", got)
	}
}
