package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TitledPanel renders body in a rounded box of exactly width columns with the
// title set into the top border ("╭─ Title ──────╮"), the look of btop-style
// dashboards. Each body line is wrapped to the inner width (ANSI-aware, so
// styled text survives) with a hanging indent matching its own leading spaces,
// so a wrapped ARN or JSON value stays under its line; the body is padded to
// minHeight lines when shorter. focused switches the border to the focus
// colour and bolds the title.
func TitledPanel(title, body string, width, minHeight int, focused bool) string {
	if width < 8 {
		width = 8
	}
	inner := width - 4 // "│ " + content + " │"
	borderC, titleC := ColorBorder(), ColorMuted()
	if focused {
		borderC, titleC = ColorBorderFocus(), ColorHeading()
	}
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(borderC))
	titleS := lipgloss.NewStyle().Foreground(lipgloss.Color(titleC)).Bold(focused)

	t := ansi.Truncate(title, max(width-6, 1), "…")
	fill := width - 5 - ansi.StringWidth(t) // "╭─ " + t + " " + fill + "╮"
	top := border.Render("╭─ ") + titleS.Render(t) + border.Render(" "+strings.Repeat("─", max(fill, 0))+"╮")

	var lines []string
	for _, src := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		lines = append(lines, wrapHanging(src, inner)...)
	}
	for len(lines) < minHeight {
		lines = append(lines, "")
	}
	var b strings.Builder
	b.WriteString(top + "\n")
	side := border.Render("│")
	for _, l := range lines {
		pad := inner - ansi.StringWidth(l)
		b.WriteString(side + " " + l + strings.Repeat(" ", max(pad, 0)) + " " + side + "\n")
	}
	b.WriteString(border.Render("╰" + strings.Repeat("─", width-2) + "╯"))
	return b.String()
}

// wrapHanging wraps one (possibly styled) line to width, indenting the
// continuation lines by the line's own leading spaces (capped at half the
// width so a deeply indented line still has room).
func wrapHanging(line string, width int) []string {
	if ansi.StringWidth(line) <= width {
		return []string{line}
	}
	plain := ansi.Strip(line)
	indent := min(len(plain)-len(strings.TrimLeft(plain, " ")), width/2)
	wrapped := strings.Split(ansi.Wrap(line, width, ""), "\n")
	out := []string{wrapped[0]}
	pad := strings.Repeat(" ", indent)
	for _, rest := range wrapped[1:] {
		// Re-wrap each continuation to the narrower width left after the indent.
		for _, piece := range strings.Split(ansi.Wrap(strings.TrimLeft(rest, " "), width-indent, ""), "\n") {
			out = append(out, pad+piece)
		}
	}
	return out
}
