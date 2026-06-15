package gluetui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/costs"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// stateClass buckets a Glue run/crawler/trigger state into a visual class, so a
// glyph and colour can be chosen consistently across the dashboard.
type stateClass int

const (
	stateNeutral stateClass = iota
	stateSuccess
	stateRunning
	stateFailure
)

// classifyState maps a Glue state string to a visual class. It is
// case-insensitive and covers job-run, crawler and trigger vocabularies.
func classifyState(state string) stateClass {
	switch strings.ToUpper(state) {
	case "SUCCEEDED", "READY", "ACTIVATED":
		return stateSuccess
	case "RUNNING", "STARTING", "STOPPING", "WAITING", "CREATED":
		return stateRunning
	case "FAILED", "ERROR", "TIMEOUT", "STOPPED":
		return stateFailure
	default:
		return stateNeutral
	}
}

// stateGlyph returns a leading glyph for a state's class.
func stateGlyph(state string) string {
	switch classifyState(state) {
	case stateSuccess:
		return "✓"
	case stateRunning:
		return "●"
	case stateFailure:
		return "✗"
	default:
		return "•"
	}
}

// stateColor returns the theme colour role for a state's class.
func stateColor(state string) string {
	switch classifyState(state) {
	case stateSuccess:
		return ui.ColorSuccess()
	case stateRunning:
		return ui.ColorAccent()
	case stateFailure:
		return ui.ColorError()
	default:
		return ui.ColorText()
	}
}

// stateLabel pairs the glyph with the state text, e.g. "✓ SUCCEEDED". An empty
// state (job never run) renders as a muted em dash.
func stateLabel(state string) string {
	if state == "" {
		return "—"
	}
	return stateGlyph(state) + " " + state
}

// formatDuration renders a run duration in seconds as "12m 22s" / "45s" /
// "1h 03m". Zero (still running, or unknown) renders as an em dash.
func formatDuration(seconds int32) string {
	if seconds <= 0 {
		return "—"
	}
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	switch {
	case h > 0:
		return fmt.Sprintf("%dh %02dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %02ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}

// formatDPUHours renders DPU-hours to two decimals, or an em dash when absent
// (a still-running or legacy run reports no DPUSeconds).
func formatDPUHours(dpuSeconds float64) string {
	if dpuSeconds <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.2f", costs.GlueRunDPUHours(dpuSeconds))
}

// formatCost renders an estimated run cost as "$0.91", or an em dash when
// DPUSeconds is absent.
func formatCost(dpuSeconds float64) string {
	if dpuSeconds <= 0 {
		return "—"
	}
	return fmt.Sprintf("$%.2f", costs.GlueRunCostUSD(dpuSeconds))
}

// shortTime renders a timestamp as "2026-06-15 01:14" in local time, or "—"
// for the zero time.
func shortTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// truncate shortens s to width runes, appending an ellipsis when it overflows.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

// runsTotals sums the DPU-hours and estimated cost across a set of runs, for
// the run-history footer.
func runsTotals(runs []JobRun) (dpuHours, costUSD float64) {
	for _, r := range runs {
		dpuHours += costs.GlueRunDPUHours(r.DPUSeconds)
		costUSD += costs.GlueRunCostUSD(r.DPUSeconds)
	}
	return dpuHours, costUSD
}
