package sqstui

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
			m := testModel(
				Queue{Name: "orders", Region: "us-east-1"},
				Queue{Name: "orders-dlq", Region: "us-east-1"},
			)
			m.Update(tea.WindowSizeMsg{Width: w, Height: h})
			return m.View().Content
		},
		StatusBar: "quit",
	})
}
