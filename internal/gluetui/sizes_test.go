package gluetui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/uitest"
)

// TestRenderSizes renders the screen at many terminal sizes and checks that
// it never panics, always fits the terminal, and keeps the status bar as the
// last line (see internal/uitest).
// Every tab is swept, since each has its own column set.
func TestRenderSizes(t *testing.T) {
	for tb := tab(0); tb < tabCount; tb++ {
		t.Run(tabNames[tb], func(t *testing.T) {
			uitest.SweepSizes(t, uitest.Sweep{
				Render: func(w, h int) string {
					mm := newGlueTestModel(w, h)
					mm.tab = tb
					mm.rebuild()
					return mm.View().Content
				},
				StatusBar: "quit",
			})
		})
	}
}
