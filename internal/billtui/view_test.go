package billtui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/billing"
)

// TestStatusBarSurvivesClip is a regression test for a header that wraps to
// more lines than the layout budgeted: the frame must still end with the
// status bar, not the column-scroll hint that sits just above it. The table is
// made wider than the terminal (width 90) so the scroll hint is non-empty — the
// exact condition under which the bug hid the status bar.
func TestStatusBarSurvivesClip(t *testing.T) {
	m := New(nil, nil, time.Now().AddDate(0, 0, -1), time.Now(), "June 2026 (month-to-date)", 5*time.Minute, "default")
	m.bill = &billing.Bill{Currency: "USD", Lines: []billing.Line{
		{Service: "Amazon EC2", UsageType: "BoxUsage", Quantity: 1, Unit: "Hrs", Amount: 1},
	}}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 16})
	v := mm.(Model)
	v.rebuild()

	lines := strings.Split(v.View(), "\n")
	last := lines[len(lines)-1]
	// The active sort now shows as a header arrow, not a status-bar label, so
	// identify the status-bar line by a key hint it always carries.
	if !strings.Contains(last, "sort") {
		t.Errorf("status bar should be the last line; got %q", last)
	}
	if strings.Contains(last, "more cols") {
		t.Errorf("last line is the scroll hint, not the status bar: %q", last)
	}
}
