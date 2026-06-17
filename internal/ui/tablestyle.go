package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// TableStyles returns the one themed style set used by every table in the
// application, so all tables render identically. Each visual aspect maps to
// its own theme role (tableHeader, tableHeaderBg, tableHeaderLine, tableText,
// tableSelectedBg, tableSelectedText) and can be customised independently in
// the settings panel or config.yaml.
func TableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		Foreground(lipgloss.Color(ColorTableHeader())).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ColorTableHeaderLine())).
		BorderBottom(true).
		Bold(true)
	if bg := ColorTableHeaderBg(); bg != "" {
		s.Header = s.Header.Background(lipgloss.Color(bg))
	}
	s.Cell = s.Cell.Foreground(lipgloss.Color(ColorTableText()))
	if bg := ColorBackground(); bg != "" {
		s.Cell = s.Cell.Background(lipgloss.Color(bg))
	}
	s.Selected = s.Selected.
		Foreground(lipgloss.Color(ColorTableSelectedText())).
		Background(lipgloss.Color(ColorTableSelectedBg())).
		Bold(true)
	// Vertical scrollbar: dim track in the table's border color, bright thumb in
	// the accent color, matching the viewport scrollbars elsewhere.
	s.ScrollTrack = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorTableBorder()))
	s.ScrollThumb = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent()))
	return s
}

// TableStylesZebra is TableStyles with zebra striping enabled: alternate data
// rows get a subtle background drawn in the theme's table-border colour. Used by
// data-grid views (the CSV/TSV table) where tracing a value to its row matters
// most; other tables keep the plain TableStyles so the look stays consistent.
func TableStylesZebra() table.Styles {
	s := TableStyles()
	if bg := ColorTableBorder(); bg != "" {
		s.RowAlt = lipgloss.NewStyle().Background(lipgloss.Color(bg))
	}
	return s
}

// TablePanelStyle returns the bordered panel every table is wrapped in, using
// the tableBorder role (borderFocus when the table has focus).
func TablePanelStyle(focused bool) lipgloss.Style {
	border := ColorTableBorder()
	if focused {
		border = ColorBorderFocus()
	}
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(border)).
		Foreground(lipgloss.Color(ColorText())).
		Padding(0, 1)
}

// TableScrollIndicator renders the "more columns" marker shown when a table is
// wider than its panel and columns are hidden off one or both edges. An empty
// string means every column is visible.
func TableScrollIndicator(t *table.Model) string {
	hiddenLeft, hiddenRight := t.ColScrollInfo()
	if hiddenLeft+hiddenRight == 0 {
		return ""
	}
	left, right := " ", " "
	if hiddenLeft > 0 {
		left = "◀"
	}
	if hiddenRight > 0 {
		right = "▶"
	}
	return MutedStyle().Render(fmt.Sprintf("%s %d more cols %s", left, hiddenLeft+hiddenRight, right))
}
