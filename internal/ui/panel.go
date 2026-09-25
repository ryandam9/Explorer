package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// TitledPanel renders body in a rounded box of exactly width columns with the
// title set into the top border ("╭─┤ Title ├──────╮"), the look of btop- and
// superfile-style dashboards. Each body line is wrapped to the inner width
// (ANSI-aware, so styled text survives) with a hanging indent matching its own
// leading spaces, so a wrapped ARN or JSON value stays under its line; the body
// is padded to minHeight lines when shorter. A body line made with Divider
// renders as a section rule. focused switches the border to the focus colour
// and bolds the title. See Box for the bottom-border info items.
func TitledPanel(title, body string, width, minHeight int, focused bool) string {
	return Box{Title: title, Focused: focused, Width: max(width, 8), MinHeight: minHeight, Wrap: true}.Render(body)
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
