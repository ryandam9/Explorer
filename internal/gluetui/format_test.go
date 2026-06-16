package gluetui

import (
	"strings"
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/table"
)

func TestFormatDuration(t *testing.T) {
	cases := map[int32]string{
		0:    "—",
		-5:   "—",
		45:   "45s",
		61:   "1m 01s",
		742:  "12m 22s",
		3661: "1h 01m",
	}
	for in, want := range cases {
		if got := formatDuration(in); got != want {
			t.Errorf("formatDuration(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestClassifyState(t *testing.T) {
	cases := map[string]stateClass{
		"SUCCEEDED": stateSuccess,
		"ready":     stateSuccess,
		"ACTIVATED": stateSuccess,
		"RUNNING":   stateRunning,
		"starting":  stateRunning,
		"FAILED":    stateFailure,
		"TIMEOUT":   stateFailure,
		"ERROR":     stateFailure,
		"":          stateNeutral,
		"WEIRD":     stateNeutral,
	}
	for in, want := range cases {
		if got := classifyState(in); got != want {
			t.Errorf("classifyState(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStateLabel(t *testing.T) {
	if got := stateLabel(""); got != "—" {
		t.Errorf("empty state = %q", got)
	}
	if got := stateLabel("FAILED"); got != "✗ FAILED" {
		t.Errorf("FAILED label = %q", got)
	}
	if got := stateLabel("SUCCEEDED"); got != "✓ SUCCEEDED" {
		t.Errorf("SUCCEEDED label = %q", got)
	}
}

func TestFormatDPUHoursAndCost(t *testing.T) {
	// 7416 DPU-seconds = 2.06 DPU-hours; at $0.44/DPU-hr ≈ $0.91.
	if got := formatDPUHours(7416); got != "2.06" {
		t.Errorf("formatDPUHours = %q, want 2.06", got)
	}
	if got := formatCost(7416); got != "$0.91" {
		t.Errorf("formatCost = %q, want $0.91", got)
	}
	if got := formatDPUHours(0); got != "—" {
		t.Errorf("zero dpu hours = %q", got)
	}
	if got := formatCost(0); got != "—" {
		t.Errorf("zero cost = %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("no-trunc = %q", got)
	}
	if got := truncate("hello world", 5); got != "hell…" {
		t.Errorf("trunc = %q", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestRunsTotals(t *testing.T) {
	runs := []JobRun{
		{DPUSeconds: 7416}, // 2.06 hrs
		{DPUSeconds: 3600}, // 1.00 hr
		{DPUSeconds: 0},    // still running, no cost
	}
	dpu, cost := runsTotals(runs)
	if dpu < 3.05 || dpu > 3.07 {
		t.Errorf("dpu total = %.4f, want ≈3.06", dpu)
	}
	if cost < 1.34 || cost > 1.35 {
		t.Errorf("cost total = %.4f, want ≈1.346", cost)
	}
}

func TestRowMatches(t *testing.T) {
	r := rowT{
		region: "us-east-1",
		cells:  table.Row{"nightly-orders-etl", "✓ SUCCEEDED"},
	}
	if !rowMatches(r, "orders") {
		t.Error("should match on name substring")
	}
	if !rowMatches(r, "east") {
		t.Error("should match on region")
	}
	if !rowMatches(r, "succeeded") {
		t.Error("should match case-insensitively on a cell")
	}
	if rowMatches(r, "failed") {
		t.Error("should not match an absent term")
	}
}

func TestShortTime(t *testing.T) {
	if got := shortTime(time.Time{}); got != "—" {
		t.Errorf("zero time = %q", got)
	}
}

func TestRedactArgs(t *testing.T) {
	in := map[string]string{
		"--TempDir":             "s3://tmp/",
		"--db-password":         "hunter2",
		"--API_KEY":             "abc",
		"--job-bookmark-option": "job-bookmark-enable",
	}
	out := redactArgs(in)
	if out["--TempDir"] != "s3://tmp/" {
		t.Errorf("non-secret altered: %q", out["--TempDir"])
	}
	if out["--db-password"] != "***" || out["--API_KEY"] != "***" {
		t.Errorf("secret not redacted: %v", out)
	}
	if redactArgs(nil) != nil {
		t.Error("nil args should yield nil")
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, k := range []string{"--db-password", "--API_KEY", "--MySecretArg", "--auth-token", "--my-credential"} {
		if !isSecretKey(k) {
			t.Errorf("%q should be secret", k)
		}
	}
	for _, k := range []string{"--TempDir", "--job-language", "--enable-metrics"} {
		if isSecretKey(k) {
			t.Errorf("%q should not be secret", k)
		}
	}
}

func TestDefBody(t *testing.T) {
	mm := &m{def: JobDef{
		Name: "etl", Role: "role/glue", GlueVersion: "4.0", Worker: "G.1X ×10",
		TimeoutMinutes: 2880, MaxRetries: 1, Script: "s3://s/etl.py",
		BookmarkEnabled:  true,
		Connections:      []string{"prod-redshift"},
		DefaultArguments: map[string]string{"--db-password": "***", "--TempDir": "s3://tmp/"},
	}}
	body := mm.defBody()
	for _, want := range []string{"role/glue", "G.1X ×10", "enabled", "s3://s/etl.py", "--db-password = ***", "prod-redshift"} {
		if !strings.Contains(body, want) {
			t.Errorf("defBody missing %q:\n%s", want, body)
		}
	}
}
