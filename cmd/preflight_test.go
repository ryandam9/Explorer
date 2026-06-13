package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/config"
)

func TestBootstrapRegion(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.AWSConfig
		want string
	}{
		{"no regions falls back", config.AWSConfig{}, "us-east-1"},
		{"first region wins", config.AWSConfig{Regions: []string{"eu-west-1", "us-east-2"}}, "eu-west-1"},
		{"allRegions flag pins us-east-1", config.AWSConfig{AllRegions: true, Regions: []string{"eu-west-1"}}, "us-east-1"},
		{"\"all\" sentinel pins us-east-1", config.AWSConfig{Regions: []string{"all"}}, "us-east-1"},
		{"empty first entry falls back", config.AWSConfig{Regions: []string{""}}, "us-east-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := bootstrapRegion(&tc.cfg); got != tc.want {
				t.Errorf("bootstrapRegion = %q, want %q", got, tc.want)
			}
		})
	}
}

// newCmdTree builds a tiny command tree mirroring the real layout closely
// enough to exercise skipPreflight's ancestor walk.
func newCmdTree() (root, configInit, s3, docs, complete *cobra.Command) {
	root = &cobra.Command{Use: "aws_explorer"}
	cfg := &cobra.Command{Use: "config"}
	configInit = &cobra.Command{Use: "init"}
	cfg.AddCommand(configInit)
	s3 = &cobra.Command{Use: "s3"}
	docs = &cobra.Command{Use: "docs"}
	complete = &cobra.Command{Use: "__complete"}
	root.AddCommand(cfg, s3, docs, complete)
	return
}

func TestSkipPreflight(t *testing.T) {
	// Ensure the offline-viewer flags don't leak between cases.
	snapshotPath = ""
	diffPaths = nil

	root, configInit, s3, docs, complete := newCmdTree()

	cases := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"root scan is checked", root, false},
		{"s3 tui is checked", s3, false},
		{"config init is skipped", configInit, true},
		{"docs is skipped", docs, true},
		{"completion driver is skipped", complete, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := skipPreflight(tc.cmd); got != tc.want {
				t.Errorf("skipPreflight(%s) = %v, want %v", tc.cmd.Name(), got, tc.want)
			}
		})
	}
}

func TestSkipPreflight_SnapshotDiff(t *testing.T) {
	// snapshot-diff is always offline (saved JSON only), so it never needs an
	// auth check regardless of which flag is set.
	sd := &cobra.Command{Use: "snapshot-diff"}

	snapshotPath, diffPaths = "", nil
	if !skipPreflight(sd) {
		t.Error("snapshot-diff should skip the auth check (always offline)")
	}

	snapshotPath = "snap.json"
	if !skipPreflight(sd) {
		t.Error("snapshot-diff --snapshot should skip the auth check (offline)")
	}

	snapshotPath = ""
	diffPaths = []string{"a.json", "b.json"}
	if !skipPreflight(sd) {
		t.Error("snapshot-diff --diff should skip the auth check (offline)")
	}
	diffPaths = nil
}
