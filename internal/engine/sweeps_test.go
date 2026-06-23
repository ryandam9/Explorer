package engine

import (
	"context"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// In single-account mode (no config.accounts), AccountSweeps returns one sweep
// carrying the engine's base config and resolved account ID — and makes no AWS
// call, so the summary command can fan tag-discovery over it safely.
func TestAccountSweeps_SingleAccount(t *testing.T) {
	e := &Engine{
		Config:    &config.Config{},
		AccountID: "111122223333",
	}
	sweeps := e.AccountSweeps(context.Background())
	if len(sweeps) != 1 {
		t.Fatalf("want 1 sweep, got %d", len(sweeps))
	}
	if sweeps[0].AccountID != "111122223333" {
		t.Errorf("sweep AccountID = %q, want the base account ID", sweeps[0].AccountID)
	}
	if sweeps[0].Err != nil {
		t.Errorf("unexpected sweep error: %v", sweeps[0].Err)
	}
}
