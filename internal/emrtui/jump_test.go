package emrtui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

func TestS3LogTarget(t *testing.T) {
	cases := []struct {
		name       string
		logURI     string
		cluster    string
		step       string
		wantBucket string
		wantPrefix string
		wantOK     bool
	}{
		{
			name: "cluster root with base path", logURI: "s3://my-logs/emr/", cluster: "j-1", step: "",
			wantBucket: "my-logs", wantPrefix: "emr/j-1/", wantOK: true,
		},
		{
			name: "step folder", logURI: "s3://my-logs/emr/", cluster: "j-1", step: "s-9",
			wantBucket: "my-logs", wantPrefix: "emr/j-1/steps/s-9/", wantOK: true,
		},
		{
			name: "bucket only, no base path", logURI: "s3://my-logs", cluster: "j-2", step: "",
			wantBucket: "my-logs", wantPrefix: "j-2/", wantOK: true,
		},
		{
			name: "trailing and leading slashes normalized", logURI: "s3://b/a/b/c/", cluster: "j-3", step: "",
			wantBucket: "b", wantPrefix: "a/b/c/j-3/", wantOK: true,
		},
		{
			name: "no log uri", logURI: "", cluster: "j-4", step: "", wantOK: false,
		},
		{
			name: "no cluster id", logURI: "s3://b/", cluster: "", step: "", wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, p, ok := s3LogTarget(tc.logURI, tc.cluster, tc.step)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if b != tc.wantBucket || p != tc.wantPrefix {
				t.Errorf("got (%q, %q), want (%q, %q)", b, p, tc.wantBucket, tc.wantPrefix)
			}
		})
	}
}

func TestS3JumpArgs(t *testing.T) {
	args := s3JumpArgs("my-logs", "emr/j-1/", "us-east-1", "prod", "/home/u/.cfg.yaml")
	joined := strings.Join(args, " ")
	for _, want := range []string{"s3", "--bucket my-logs", "--prefix emr/j-1/", "--region us-east-1", "--profile prod", "--config /home/u/.cfg.yaml"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}

	// Empty prefix / global region are omitted.
	args = s3JumpArgs("b", "", "global", "", "")
	joined = strings.Join(args, " ")
	if strings.Contains(joined, "--prefix") {
		t.Errorf("empty prefix should be omitted: %v", args)
	}
	if strings.Contains(joined, "--region") {
		t.Errorf("global region should be omitted: %v", args)
	}
}

// TestS3JumpDropsMissingConfig is a regression guard for the bug where the
// dashboard forwarded its resolved config path to the child s3/cw process even
// when running on built-in defaults (no file on disk). An explicit --config is
// fatal-if-missing, so every "jump to logs" press made the child abort with
// "Error reading config file: ... no such file or directory". The jump now runs
// the path through ui.ConfigArgPath; this exercises that exact composition.
func TestS3JumpDropsMissingConfig(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "config.yaml") // never created — built-in defaults
	existing := filepath.Join(dir, "real.yaml")
	if err := os.WriteFile(existing, []byte("ui: {}\n"), 0o644); err != nil {
		t.Fatalf("seeding config: %v", err)
	}

	// Built-in defaults: the non-existent path must not be forwarded, or the
	// child s3 process would die before it could browse the logs.
	args := s3JumpArgs("b", "p/", "us-east-1", "", ui.ConfigArgPath(missing))
	if strings.Contains(strings.Join(args, " "), "--config") {
		t.Errorf("missing config must not be forwarded: %v", args)
	}

	// A real config file is still forwarded so the child shares the user's settings.
	args = s3JumpArgs("b", "p/", "us-east-1", "", ui.ConfigArgPath(existing))
	if !strings.Contains(strings.Join(args, " "), "--config "+existing) {
		t.Errorf("existing config should be forwarded: %v", args)
	}
}
