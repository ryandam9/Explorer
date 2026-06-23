package cmd

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestStampAccount(t *testing.T) {
	// Non-empty acct overwrites every resource (matching the typed-collector
	// stamping), so tag-discovered resources carry the same account identifier.
	rs := []model.Resource{{ID: "a"}, {ID: "b", AccountID: "old"}}
	stampAccount(rs, "prod")
	for _, r := range rs {
		if r.AccountID != "prod" {
			t.Errorf("resource %q AccountID = %q, want prod", r.ID, r.AccountID)
		}
	}

	// An empty acct (account ID unresolved and no name) leaves resources as-is.
	keep := []model.Resource{{ID: "c", AccountID: "keep"}}
	stampAccount(keep, "")
	if keep[0].AccountID != "keep" {
		t.Errorf("empty acct must not clear existing AccountID, got %q", keep[0].AccountID)
	}
}
