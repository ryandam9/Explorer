package s3tui

import (
	"errors"
	"testing"

	"github.com/aws/smithy-go"
)

// bucketConfigSentinel must keep "genuinely not configured" distinct from
// "denied" and "failed" — a denied/failed read must never render as the
// negative fact (#375, CLAUDE.md §6a/§8).
func TestBucketConfigSentinel(t *testing.T) {
	const nfCode = "NoSuchWebsiteConfiguration"
	const nfLabel = "Not configured"

	cases := []struct {
		name string
		err  error
		nf   string
		want string
	}{
		{"genuine not-found → label", &smithy.GenericAPIError{Code: nfCode}, nfCode, nfLabel},
		{"access denied → denied sentinel", &smithy.GenericAPIError{Code: "AccessDenied"}, nfCode, "Access Denied"},
		{"other API error → unknown sentinel", &smithy.GenericAPIError{Code: "SlowDown"}, nfCode, "—"},
		{"non-API error → unknown sentinel", errors.New("dial tcp: i/o timeout"), nfCode, "—"},
		// Calls with no not-found code (e.g. intelligent-tiering): a denial is
		// still denial; anything else is unknown — never the empty "None".
		{"no nf-code, denied", &smithy.GenericAPIError{Code: "AccessDenied"}, "", "Access Denied"},
		{"no nf-code, other error", &smithy.GenericAPIError{Code: "SlowDown"}, "", "—"},
		// A not-found code must not be mistaken for the negative when it doesn't match.
		{"unrelated code is not the label", &smithy.GenericAPIError{Code: "InternalError"}, nfCode, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := bucketConfigSentinel(c.err, c.nf, nfLabel); got != c.want {
				t.Errorf("bucketConfigSentinel(%v, %q) = %q, want %q", c.err, c.nf, got, c.want)
			}
		})
	}
}
