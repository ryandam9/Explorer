package emrtui

import (
	"testing"

	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

// The picker's UI types must match the EMR SDK enum values exactly, or the
// presigned-URL call rejects them.
func TestAppUIOptionsMatchSDKEnums(t *testing.T) {
	want := map[string]bool{
		string(emrtypes.PersistentAppUITypeShs): true,
		string(emrtypes.PersistentAppUITypeYts): true,
		string(emrtypes.PersistentAppUITypeTez): true,
	}
	if len(appUIOptions) != len(want) {
		t.Fatalf("appUIOptions has %d entries, want %d", len(appUIOptions), len(want))
	}
	for _, opt := range appUIOptions {
		if !want[opt.UIType] {
			t.Errorf("appUIOption %q has unknown UIType %q", opt.Label, opt.UIType)
		}
		if opt.Label == "" {
			t.Errorf("appUIOption with type %q has empty label", opt.UIType)
		}
	}
}
