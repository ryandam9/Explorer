package table

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func rows(n int) []Row {
	out := make([]Row, n)
	for i := range out {
		out[i] = Row{"r", "value"}
	}
	return out
}

// When there are more rows than fit, the table draws a vertical scrollbar
// (issue #155); when they all fit, it does not.
func TestVerticalScrollbarVisibility(t *testing.T) {
	cols := []Column{{Title: "#", Width: 3}, {Title: "V", Width: 8}}

	full := New(WithColumns(cols), WithRows(rows(40)), WithFocused(true))
	full.SetWidth(30)
	full.SetHeight(11) // viewport height 10 < 40 rows
	view := full.View()
	if !strings.ContainsAny(view, "┃│") {
		t.Errorf("a table with more rows than fit should show a scrollbar:\n%s", view)
	}

	fits := New(WithColumns(cols), WithRows(rows(3)), WithFocused(true))
	fits.SetWidth(30)
	fits.SetHeight(11)
	if v := fits.View(); strings.ContainsAny(v, "┃│") {
		t.Errorf("a table whose rows all fit should not show a scrollbar:\n%s", v)
	}
}

// The scrollbar gutter is taken out of the content width, so the rendered rows
// (bar included) never exceed the width the caller set.
func TestScrollbarStaysWithinWidth(t *testing.T) {
	cols := []Column{{Title: "#", Width: 3}, {Title: "V", Width: 8}}
	m := New(WithColumns(cols), WithRows(rows(40)), WithFocused(true))
	const width = 30
	m.SetWidth(width)
	m.SetHeight(11)
	for _, ln := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(ln); w > width {
			t.Errorf("line width %d exceeds table width %d: %q", w, width, ln)
		}
	}
}

// The thumb tracks the scroll position: top at the start, lower after scrolling.
func TestScrollbarThumbTracksPosition(t *testing.T) {
	thumbFirstRow := func(s string) int {
		for i, ln := range strings.Split(s, "\n") {
			if strings.Contains(ln, "┃") {
				return i
			}
		}
		return -1
	}

	cols := []Column{{Title: "#", Width: 3}, {Title: "V", Width: 8}}
	m := New(WithColumns(cols), WithRows(rows(60)), WithFocused(true))
	m.SetWidth(30)
	m.SetHeight(11)

	top := thumbFirstRow(m.View())
	if top < 0 {
		t.Fatal("expected a thumb at the top")
	}
	m.GotoBottom()
	bottom := thumbFirstRow(m.View())
	if bottom <= top {
		t.Errorf("scrolling to the bottom should lower the thumb: top=%d bottom=%d", top, bottom)
	}
}

func TestRenderVScrollbarBlankWhenContentFits(t *testing.T) {
	plain := ansi.Strip(RenderVScrollbar(5, 8, 8, 0, lipgloss.NewStyle(), lipgloss.NewStyle()))
	for i, ln := range strings.Split(plain, "\n") {
		if strings.TrimSpace(ln) != "" {
			t.Errorf("line %d should be blank when content fits, got %q", i, ln)
		}
	}
}
