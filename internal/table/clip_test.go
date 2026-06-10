package table

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// stripANSIForTest removes ANSI escape sequences so cell widths can be measured.
func stripANSIForTest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// TestRenderNeverExceedsWidth guards against the row-wrapping bug: a column that
// auto-fits to content wider than the whole view must be clipped to the table
// width, never overflow (which makes the surrounding panel wrap the row).
func TestRenderNeverExceedsWidth(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 3},
		{Title: "ID", Width: 10},
		{Title: "Name", Width: 8},
	}
	rows := []Row{
		{"1", "short", "a"},
		{"2", strings.Repeat("x", 200), "b"}, // far wider than the view
	}
	m := New(WithColumns(cols), WithRows(rows), WithFocused(true))
	const width = 30
	m.SetWidth(width)
	m.SetHeight(8)

	// Scroll the wide ID column into view as the first scrollable column.
	for i := 0; i < 3; i++ {
		m.ScrollRight()
	}

	for _, line := range strings.Split(m.View(), "\n") {
		if w := runewidth.StringWidth(stripANSIForTest(line)); w > width {
			t.Errorf("rendered line width %d exceeds table width %d: %q", w, width, stripANSIForTest(line))
		}
	}
}
