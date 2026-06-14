package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// VScrollbar renders a one-column vertical scrollbar of the given height: a
// thumb sized to the visible fraction of the content, positioned by the scroll
// offset. When everything fits (total <= visible) it returns a blank gutter of
// spaces so callers can reserve the column unconditionally and avoid the
// content reflowing the moment a scrollbar appears.
//
// total is the content's line count, visible the number of lines on screen and
// offset the index of the topmost visible line. The geometry lives in the table
// package (which cannot import ui); this wires the theme colors to it.
func VScrollbar(height, total, visible, offset int) string {
	track := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	thumb := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent()))
	return table.RenderVScrollbar(height, total, visible, offset, track, thumb)
}
