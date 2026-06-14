package audittui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/findings"
)

// TestStatusBarSurvivesClip mirrors the billtui regression: the audit TUI
// shared the same header-height miscount, which clipped the status bar off the
// bottom whenever the findings table was wider than the terminal.
func TestStatusBarSurvivesClip(t *testing.T) {
	m := New([]string{"us-east-1"}, false, nil)
	m.scanning = false
	m.all = []findings.Finding{{ID: "COST-EBS-001", Resource: "vol-1", Region: "us-east-1", Title: "unattached"}}

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
