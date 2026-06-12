package acctsnap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Baselines live under ~/.aws_explorer/account-snapshots/<account-id>/, one
// file per region scope, mirroring the VPC explorer's vpc-snapshots/ layout.

// accountDir returns the snapshot directory for an account, creating it when
// asked to. An empty account ID (credentials that can't call STS) still gets
// a stable directory rather than an error.
func accountDir(accountID string, create bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if accountID == "" {
		accountID = "unknown-account"
	}
	dir := filepath.Join(home, ".aws_explorer", "account-snapshots", accountID)
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Save writes the snapshot as the baseline for its account + region scope,
// returning the file path.
func Save(snap Snapshot) (string, error) {
	dir, err := accountDir(snap.AccountID, true)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, ScopeKey(snap.Regions)+".json")
	return path, os.WriteFile(path, data, 0o644)
}

// Load reads the baseline for an account + region scope. The bool is false
// when no baseline has been saved for that exact scope — use Scopes to tell
// the user which scopes do have baselines.
func Load(accountID string, regions []string) (Snapshot, bool, error) {
	dir, err := accountDir(accountID, false)
	if err != nil {
		return Snapshot{}, false, err
	}
	data, err := os.ReadFile(filepath.Join(dir, ScopeKey(regions)+".json"))
	if os.IsNotExist(err) {
		return Snapshot{}, false, nil
	}
	if err != nil {
		return Snapshot{}, false, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, false, err
	}
	return snap, true, nil
}

// Scopes lists the region-scope keys that have saved baselines for the
// account, sorted. Used to warn when a diff is requested under a different
// scope than the baseline was taken with.
func Scopes(accountID string) []string {
	dir, err := accountDir(accountID, false)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".json"); ok && !e.IsDir() {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
