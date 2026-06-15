package gluetui

import (
	"testing"
	"time"
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

func TestResolveWidths(t *testing.T) {
	specs := []colSpec{{"NAME", 0}, {"STATE", 10}, {"DUR", 6}}
	// total 40: gaps=2, fixed=10+6+2=18, flex=40-18=22.
	widths := resolveWidths(specs, 40)
	if widths[0] != 22 || widths[1] != 10 || widths[2] != 6 {
		t.Errorf("widths = %v, want [22 10 6]", widths)
	}
	// Flex floors at 8 when space is tight.
	tight := resolveWidths(specs, 10)
	if tight[0] != 8 {
		t.Errorf("flex floor = %d, want 8", tight[0])
	}
}

func TestRowMatches(t *testing.T) {
	r := rowT{
		region: "us-east-1",
		cells:  []cell{{text: "nightly-orders-etl"}, {text: "✓ SUCCEEDED"}},
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
