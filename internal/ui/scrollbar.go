package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// VScrollbar renders a one-column vertical scrollbar of the given height: a
// thumb sized to the visible fraction of the content, positioned by the scroll
// offset. When everything fits (total <= visible) it returns a blank gutter of
// spaces so callers can reserve the column unconditionally and avoid the
// content reflowing the moment a scrollbar appears.
//
// total is the content's line count, visible the number of lines on screen and
// offset the index of the topmost visible line.
func VScrollbar(height, total, visible, offset int) string {
	if height < 1 {
		return ""
	}
	blank := strings.TrimRight(strings.Repeat(" \n", height), "\n")
	if total <= visible || visible < 1 {
		return blank
	}

	// Thumb height is proportional to how much of the content is on screen,
	// never smaller than one cell so it stays visible in very long scrolls.
	thumb := height * visible / total
	if thumb < 1 {
		thumb = 1
	}
	if thumb > height {
		thumb = height
	}

	maxOffset := total - visible
	travel := height - thumb
	pos := 0
	if maxOffset > 0 {
		pos = travel * offset / maxOffset
	}
	if pos > travel {
		pos = travel
	}
	if pos < 0 {
		pos = 0
	}

	trackStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	thumbStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent()))

	var b strings.Builder
	for i := 0; i < height; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		if i >= pos && i < pos+thumb {
			b.WriteString(thumbStyle.Render("┃"))
		} else {
			b.WriteString(trackStyle.Render("│"))
		}
	}
	return b.String()
}
