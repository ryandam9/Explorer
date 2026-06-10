package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ClipToSize hard-clips a rendered frame to the terminal size: lines wider
// than width are ANSI-aware truncated, and lines beyond height are dropped
// from the bottom. Bubble Tea trims an over-tall frame from the TOP, and a
// line wider than the terminal wraps and scrolls everything up — both hide
// the header — so every browser passes its final frame through this guard.
func ClipToSize(view string, width, height int) string {
	if width <= 0 || height <= 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	clipped := false
	if len(lines) > height {
		lines = lines[:height]
		clipped = true
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > width {
			lines[i] = ansi.Truncate(l, width, "")
			clipped = true
		}
	}
	if !clipped {
		return view
	}
	return strings.Join(lines, "\n")
}
