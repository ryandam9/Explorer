package emrtui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/uitest"
)

// TestRenderSizes renders the screen at many terminal sizes and checks that
// it never panics, always fits the terminal, and keeps the status bar as the
// last line (see internal/uitest).
func TestRenderSizes(t *testing.T) {
	uitest.SweepSizes(t, uitest.Sweep{
		Render:    func(w, h int) string { return newClusterTestModel(w, h).View().Content },
		StatusBar: "quit",
	})
}
