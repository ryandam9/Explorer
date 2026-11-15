package s3tui

import (
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"
)

func TestObjectLockErrValue(t *testing.T) {
	api := func(code, msg string) error {
		return fmt.Errorf("operation error S3: %w", &smithy.GenericAPIError{Code: code, Message: msg})
	}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"object has no retention", api("NoSuchObjectLockConfiguration", "no config"), "Not set"},
		{"bucket lock not found", api("ObjectLockConfigurationNotFoundError", "nope"), "Not set"},
		{"bucket without object lock", api("InvalidRequest", "Bucket is missing Object Lock Configuration"), "Not set"},
		{"unrelated invalid request", api("InvalidRequest", "something else"), "—"},
		{"denied", api("AccessDenied", "Access Denied"), "Access Denied"},
		{"wrong region redirect", api("PermanentRedirect", "use the right endpoint"), "—"},
		{"non-API error", errors.New("dial tcp: i/o timeout"), "—"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := objectLockErrValue(tt.err); got != tt.want {
				t.Errorf("objectLockErrValue() = %q, want %q", got, tt.want)
			}
		})
	}
}
