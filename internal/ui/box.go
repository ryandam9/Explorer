package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// Box is the one bordered-panel renderer behind TitledPanel and TablePanel.
// It draws the superfile-style frame: the title set into the top border,
// short status items ("3/120", "◀ 2 cols ▶") set into the bottom border, and
// optional section dividers inside:
//
//	╭─┤ Functions (42) ├──────────────╮
//	│ NAME        RUNTIME             │
//	├─ Networking ────────────────────┤
//	│ …                               │
//	╰────────┤ ◀ 2 more cols ▶ ├─┤ 3/42 ├─╯
//
// Every line is exactly Width columns, so callers can measure the panel with
// lipgloss.Height/Width like any other block (CLAUDE.md §9).
type Box struct {
	Title string
	// Info items sit right-aligned in the bottom border. When they don't all
	// fit, the leftmost are dropped first — put the most important last.
	Info    []string
	Focused bool
	// Width is the total width including the border. 0 fits the widest body
	// line (plus the border and one column of padding each side).
	Width int
	// MinHeight pads the body with blank lines up to this many lines.
	MinHeight int
	// Height, when > 0, fixes the body at exactly this many lines: shorter
	// bodies are padded, longer ones cut (for panels sized by the layout).
	Height int
	// Wrap soft-wraps body lines wider than the inner width (with a hanging
	// indent); otherwise they are cut, which is right for pre-sized content
	// like a table view.
	Wrap bool
	// BorderColor overrides the border colour ("" = border / borderFocus).
	BorderColor string
}

// dividerMark prefixes a body line that should render as a section divider.
const dividerMark = "\x1d"

// Divider returns a body line that Box renders as a full-width section rule,
// with label set into it when non-empty ("├─ Networking ──────┤").
func Divider(label string) string { return dividerMark + label }

// Border runes, shared by every panel so the frame is uniform.
const (
	bTL, bTR, bBL, bBR = "╭", "╮", "╰", "╯"
	bH, bV             = "─", "│"
	bML, bMR           = "├", "┤"
)

func (b Box) styles() (border, title, info lipgloss.Style) {
	bc := b.BorderColor
	if bc == "" {
		bc = ColorBorder()
		if b.Focused {
			bc = ColorBorderFocus()
		}
	}
	tc := ColorMuted()
	if b.Focused {
		tc = ColorHeading()
	}
	border = lipgloss.NewStyle().Foreground(lipgloss.Color(bc))
	title = lipgloss.NewStyle().Foreground(lipgloss.Color(tc)).Bold(b.Focused)
	info = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	return border, title, info
}

// Render frames body.
func (b Box) Render(body string) string {
	src := strings.Split(strings.TrimRight(body, "\n"), "\n")
	width := b.Width
	if width <= 0 {
		for _, l := range src {
			if !strings.HasPrefix(l, dividerMark) {
				width = max(width, ansi.StringWidth(l))
			}
		}
		width += 4
	}
	width = max(width, 8)
	inner := width - 4 // "│ " + content + " │"

	var lines []string
	for _, l := range src {
		switch {
		case strings.HasPrefix(l, dividerMark):
			lines = append(lines, l)
		case b.Wrap:
			lines = append(lines, wrapHanging(l, inner)...)
		default:
			lines = append(lines, ansi.Truncate(l, inner, ""))
		}
	}
	for len(lines) < max(b.MinHeight, b.Height) {
		lines = append(lines, "")
	}
	if b.Height > 0 && len(lines) > b.Height {
		lines = lines[:b.Height]
	}

	border, titleS, infoS := b.styles()
	var out strings.Builder
	out.WriteString(boxTop(width, b.Title, border, titleS) + "\n")
	side := border.Render(bV)
	for _, l := range lines {
		if strings.HasPrefix(l, dividerMark) {
			out.WriteString(boxDivider(width, strings.TrimPrefix(l, dividerMark), border, titleS) + "\n")
			continue
		}
		pad := inner - ansi.StringWidth(l)
		out.WriteString(side + " " + l + strings.Repeat(" ", max(pad, 0)) + " " + side + "\n")
	}
	out.WriteString(boxBottom(width, b.Info, border, infoS))
	return out.String()
}

// boxTop renders "╭─┤ Title ├────╮" (or a plain rule without a title).
func boxTop(width int, title string, border, titleS lipgloss.Style) string {
	if title == "" || width < 8 {
		return border.Render(bTL + strings.Repeat(bH, width-2) + bTR)
	}
	t := ansi.Truncate(title, width-8, "…")
	fill := width - 7 - ansi.StringWidth(t) // "╭─┤ " + t + " ├" + fill + "╮"
	return border.Render(bTL+bH+bMR+" ") + titleS.Render(t) +
		border.Render(" "+bML+strings.Repeat(bH, max(fill, 0))+bTR)
}

// boxDivider renders an inner section rule "├─ Label ─────┤".
func boxDivider(width int, label string, border, labelS lipgloss.Style) string {
	if label == "" {
		return border.Render(bML + strings.Repeat(bH, width-2) + bMR)
	}
	l := ansi.Truncate(label, width-6, "…")
	fill := width - 5 - ansi.StringWidth(l) // "├─ " + l + " " + fill + "┤"
	return border.Render(bML+bH+" ") + labelS.Render(l) + border.Render(" "+strings.Repeat(bH, max(fill, 0))+bMR)
}

// boxBottom renders "╰──────┤ a ├─┤ b ├─╯", dropping the leftmost items that
// don't fit.
func boxBottom(width int, items []string, border, infoS lipgloss.Style) string {
	var keep []string
	for _, it := range items {
		if strings.TrimSpace(it) != "" {
			keep = append(keep, it)
		}
	}
	used := func(its []string) int {
		n := 2 // trailing "─╯"
		for i, it := range its {
			n += ansi.StringWidth(it) + 4 // "┤ " + it + " ├"
			if i > 0 {
				n++ // "─" between items
			}
		}
		return n
	}
	for len(keep) > 0 && used(keep) > width-2 { // leave "╰" plus at least one "─"
		keep = keep[1:]
	}
	if len(keep) == 0 {
		return border.Render(bBL + strings.Repeat(bH, width-2) + bBR)
	}
	var b strings.Builder
	b.WriteString(border.Render(bBL + strings.Repeat(bH, width-1-used(keep))))
	for i, it := range keep {
		if i > 0 {
			b.WriteString(border.Render(bH))
		}
		b.WriteString(border.Render(bMR+" ") + infoS.Render(it) + border.Render(" "+bML))
	}
	b.WriteString(border.Render(bH + bBR))
	return b.String()
}

// TablePanel wraps a shared-table view in the standard table frame: title in
// the top border, and in the bottom border the caller's info items followed by
// the hidden-column marker and the row position ("3/120"). It replaces the
// TablePanelStyle + TableScrollIndicator pair, so the "more columns" line under
// the table is no longer needed and that row goes back to the table. The frame
// is the same height as TablePanelStyle's (two border lines).
func TablePanel(t *table.Model, focused bool, title string, info ...string) string {
	return TableBox(t, focused, title, info...).Render(t.View())
}

// TableBox is the Box TablePanel draws, for callers that frame more than the
// bare table view (a filter line under it, a fixed panel size).
func TableBox(t *table.Model, focused bool, title string, info ...string) Box {
	items := append([]string{}, info...)
	if hl, hr := t.ColScrollInfo(); hl+hr > 0 {
		left, right := "", ""
		if hl > 0 {
			left = "◀ "
		}
		if hr > 0 {
			right = " ▶"
		}
		items = append(items, fmt.Sprintf("%s%d more cols%s", left, hl+hr, right))
	}
	items = append(items, TablePosition(t))
	bc := ColorTableBorder()
	if focused {
		bc = ColorBorderFocus()
	}
	return Box{Title: title, Info: items, Focused: focused, BorderColor: bc}
}

// TablePosition is the "cursor/rows" counter shown in a table panel's bottom
// border ("0/0" when empty).
func TablePosition(t *table.Model) string {
	n := len(t.Rows())
	if n == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", t.Cursor()+1, n)
}

// SectionHeading renders a heading followed by a rule out to width
// ("Services ──────────"), the sidebar section style of superfile. The label is
// cut to fit; the rule takes whatever is left.
func SectionHeading(label string, width int) string {
	l := ansi.Truncate(label, max(width-2, 1), "…")
	rule := width - ansi.StringWidth(l) - 1
	out := PanelTitleStyle().Render(l)
	if rule > 0 {
		out += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder())).Render(strings.Repeat(bH, rule))
	}
	return out
}
