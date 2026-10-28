package trailtui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ryandam9/aws_explorer/internal/uitest"
)

// TestRenderSizes renders the screen at many terminal sizes and checks that
// it never panics, always fits the terminal, and keeps the status bar as the
// last line (see internal/uitest).
func TestRenderSizes(t *testing.T) {
	uitest.SweepSizes(t, uitest.Sweep{
		Render: func(w, h int) string {
			mm, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: w, Height: h})
			return mm.(Model).View().Content
		},
		StatusBar: "quit",
	})
}
