// Package uitest holds shared test helpers for the TUIs. Import it only from
// _test.go files.
//
// SweepSizes renders a screen at many terminal sizes and checks the frame
// contract every TUI must keep (CLAUDE.md §9): no panic, the frame fits the
// terminal, and the status bar is still the last line. Hand-computed layout
// heights are this repo's most frequent UI bug (#99, #208, #218); a fixed-size
// test only catches them at the one size it happens to use.
//
// The idea comes from velo's TestRenderSizes (github.com/bjarneo/velo, MIT).
package uitest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Widths and Heights are the default sweep: from cramped to very large.
var (
	Widths  = []int{1, 20, 40, 60, 80, 100, 120, 160, 220}
	Heights = []int{1, 5, 10, 16, 24, 40, 60}
)

// Sweep configures SweepSizes.
type Sweep struct {
	// Render builds the screen at w×h and returns View().Content. It is
	// called with a fresh model for every size.
	Render func(w, h int) string

	// StatusBar is text the status bar always shows (a key hint such as
	// "help"). When set, the last line of the frame must contain it.
	StatusBar string

	// MinW×MinH (default 80×16) is the smallest size at which the status bar
	// and border checks apply. Below it, hints elide and panels may hit
	// their minimum widths, so only the panic and fit checks run.
	MinW, MinH int

	// Widths and Heights override the default sweep.
	Widths, Heights []int
}

// SweepSizes runs s.Render at every size and reports each failure with the
// size it happened at.
func SweepSizes(t *testing.T, s Sweep) {
	t.Helper()
	widths, heights := s.Widths, s.Heights
	if widths == nil {
		widths = Widths
	}
	if heights == nil {
		heights = Heights
	}
	minW, minH := s.MinW, s.MinH
	if minW == 0 {
		minW = 80
	}
	if minH == 0 {
		minH = 16
	}
	for _, w := range widths {
		for _, h := range heights {
			out, err := render(s.Render, w, h)
			if err != nil {
				t.Errorf("%dx%d: %v", w, h, err)
				continue
			}
			for _, p := range CheckFrame(out, w, h) {
				t.Errorf("%dx%d: %s", w, h, p)
			}
			if w < minW || h < minH {
				continue
			}
			for _, p := range CheckBorders(out) {
				t.Errorf("%dx%d: %s", w, h, p)
			}
			if s.StatusBar != "" {
				lines := strings.Split(out, "\n")
				last := ansi.Strip(lines[len(lines)-1])
				if !strings.Contains(last, s.StatusBar) {
					t.Errorf("%dx%d: status bar (%q) is not the last line; last line = %q",
						w, h, s.StatusBar, last)
				}
			}
		}
	}
}

// render calls f and turns a panic into an error.
func render(f func(w, h int) string, w, h int) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("render panicked: %v", r)
		}
	}()
	return f(w, h), nil
}

// CheckFrame returns the ways a rendered frame breaks the w×h terminal: too
// many lines (Bubble Tea then cuts the header off the top) or a line wider
// than the terminal (it wraps and scrolls everything up).
func CheckFrame(out string, w, h int) []string {
	var problems []string
	lines := strings.Split(out, "\n")
	if len(lines) > h {
		problems = append(problems, fmt.Sprintf("frame is %d lines, terminal is %d", len(lines), h))
	}
	for i, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			problems = append(problems, fmt.Sprintf("line %d is %d cells, terminal is %d", i, lw, w))
			break // one is enough; the rest are usually the same cause
		}
	}
	return problems
}

// CheckBorders reports lines where a rounded panel corner has no partner:
// more "╭" than "╮", or more "╰" than "╯". ClipToSize keeps an over-wide
// frame inside the terminal, so a panel drawn too wide shows up only as a
// missing right border. This catches it.
func CheckBorders(out string) []string {
	var problems []string
	for i, l := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Count(l, "╭") != strings.Count(l, "╮") ||
			strings.Count(l, "╰") != strings.Count(l, "╯") {
			problems = append(problems, fmt.Sprintf("line %d has an unclosed panel border (panel clipped?): %q", i, l))
			break
		}
	}
	return problems
}
