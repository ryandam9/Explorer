package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Overlay composites fg over bg with fg's top-left corner at cell (x, y),
// preserving the background around the foreground block — a floating HUD
// panel rather than a screen swap. Both strings are ANSI-styled multiline
// blocks; widths are measured in terminal cells.
func Overlay(bg, fg string, x, y int) string {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")

	for i, fl := range fgLines {
		row := y + i
		for row >= len(bgLines) {
			bgLines = append(bgLines, "")
		}
		flW := ansi.StringWidth(fl)
		if flW == 0 {
			continue
		}
		line := bgLines[row]
		left := ansi.Truncate(line, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(line, x+flW, "")
		bgLines[row] = left + fl + right
	}
	return strings.Join(bgLines, "\n")
}

// OverlayCenter centers fg over bg, treating bg as a width×height frame.
func OverlayCenter(bg, fg string, width, height int) string {
	x := (width - lipgloss.Width(fg)) / 2
	y := (height - lipgloss.Height(fg)) / 2
	return Overlay(bg, fg, x, y)
}

// OverlayCenterBlank centers fg on an otherwise-blank width×height screen, so a
// mouse text-selection over a modal overlay picks up only its content — never
// the underlying view behind it (issue #284). The rows around the overlay are
// empty, so dragging across them selects nothing meaningful.
//
// Use this for modal overlays (about/help/detail/preview/pickers/confirms,
// settings). Use the see-through OverlayCenter only for HUD panels that must
// stay readable over the live app (the debug pane).
func OverlayCenterBlank(fg string, width, height int) string {
	if height < 1 {
		height = 1
	}
	bg := strings.Repeat("\n", height-1) // height empty lines
	return OverlayCenter(bg, fg, width, height)
}
