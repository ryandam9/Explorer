package s3tui

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"
)

// A zero-byte object answers a ranged GET with 416 InvalidRange; that must be
// recognised as "empty", not surfaced as a preview failure (issue #312).
func TestIsEmptyObjectRangeErr(t *testing.T) {
	invalidRange := &smithy.GenericAPIError{Code: "InvalidRange", Message: "The requested range is not satisfiable"}
	if !isEmptyObjectRangeErr(invalidRange) {
		t.Errorf("InvalidRange should be treated as an empty object")
	}

	other := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
	if isEmptyObjectRangeErr(other) {
		t.Errorf("AccessDenied must not be treated as an empty object")
	}

	if isEmptyObjectRangeErr(errors.New("boom")) {
		t.Errorf("a plain error must not be treated as an empty object")
	}

	if isEmptyObjectRangeErr(nil) {
		t.Errorf("nil must not be treated as an empty object")
	}
}
