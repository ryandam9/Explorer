package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/user/aws_explorer/internal/table"
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
