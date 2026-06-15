package emrtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// stateClass buckets an EMR cluster or step state into a visual class, so a
// glyph and colour can be chosen consistently across the dashboard.
type stateClass int

const (
	stateNeutral stateClass = iota
	stateSuccess
	stateRunning
	stateFailure
)

// classifyState maps an EMR cluster/step state to a visual class. It is
// case-insensitive and covers both the cluster vocabulary (STARTING,
// BOOTSTRAPPING, RUNNING, WAITING, TERMINATING, TERMINATED,
// TERMINATED_WITH_ERRORS) and the step vocabulary (PENDING, RUNNING, COMPLETED,
// CANCELLED, FAILED, INTERRUPTED).
func classifyState(state string) stateClass {
	switch strings.ToUpper(state) {
	case "COMPLETED", "WAITING":
		return stateSuccess
	case "STARTING", "BOOTSTRAPPING", "RUNNING", "TERMINATING", "PENDING", "CANCEL_PENDING":
		return stateRunning
	case "FAILED", "TERMINATED_WITH_ERRORS", "CANCELLED", "INTERRUPTED":
		return stateFailure
	default:
		// TERMINATED and anything unknown render muted.
		return stateNeutral
	}
}

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

// stateLabel pairs the glyph with the state text, e.g. "✓ COMPLETED". An empty
// state renders as a muted em dash.
func stateLabel(state string) string {
	if state == "" {
		return "—"
	}
	return stateGlyph(state) + " " + state
}

// formatDuration renders a duration between two times as "12m 02s" / "45s" /
// "1h 03m". A zero end (still running / unknown) renders as an em dash.
func formatDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return "—"
	}
	return formatSeconds(int32(end.Sub(start).Seconds()))
}

func formatSeconds(seconds int32) string {
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

// shortTime renders a timestamp as "2026-06-15 01:14" in local time, or "—" for
// the zero time.
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
