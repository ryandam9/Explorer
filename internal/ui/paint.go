package ui

import (
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Painted background (ui.paintBackground). By default every TUI lets the
// terminal's own background show through. With painting on, the whole screen
// is filled with the theme's canvas colour, the look of superfile and btop.
//
// Painting is one pass over the finished frame rather than a Background() on
// every style: in lipgloss v1 each styled segment ends with a full SGR reset,
// which would punch a hole of terminal background through any style that did
// not set its own. Paint re-applies the canvas after every reset (and every
// "default background" code), starts each line on it and pads each line to the
// full width, so unstyled gaps, padding and blank lines are all covered while
// styles that do set a background (the status bar, the selected row) keep it.

var paintBG atomic.Bool

// SetPaintBackground turns full-screen background painting on or off.
func SetPaintBackground(on bool) { paintBG.Store(on) }

// PaintBackgroundEnabled reports whether painting is on.
func PaintBackgroundEnabled() bool { return paintBG.Load() }

// sgrResets matches the SGR sequences after which the background is the
// terminal default: a full reset ("\x1b[0m" / "\x1b[m") or "default
// background" ("\x1b[49m").
var sgrResets = regexp.MustCompile(`\x1b\[(?:0?|49)m`)

// Paint fills frame with the active theme's canvas colour when painting is on
// (otherwise it returns frame unchanged). width and height are the terminal
// size; short lines are padded to width and a short frame gets blank painted
// lines up to height. It is a no-op when the terminal has no colour support or
// the theme has no canvas.
func Paint(frame string, width, height int) string {
	if !paintBG.Load() {
		return frame
	}
	canvas := ColorCanvas()
	if canvas == "" {
		return frame
	}
	seq := lipgloss.ColorProfile().Color(canvas).Sequence(true)
	if seq == "" {
		return frame // monochrome terminal: nothing to paint with
	}
	on := "\x1b[" + seq + "m"
	return paintWith(frame, on, width, height)
}

func paintWith(frame, on string, width, height int) string {
	lines := strings.Split(frame, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	var b strings.Builder
	b.Grow(len(frame) + len(lines)*(2*len(on)+8))
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(on)
		b.WriteString(sgrResets.ReplaceAllStringFunc(l, func(m string) string { return m + on }))
		if pad := width - ansi.StringWidth(l); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
