package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"
)

// HelpRow is one key/action line of a help overlay.
type HelpRow struct {
	Key    string
	Action string
}

// HelpSection is one titled block of key/action rows.
type HelpSection struct {
	Title string
	Rows  []HelpRow
}

// helpKeyColW is the fixed width of the key column, matching the historical
// single-column help layout.
const helpKeyColW = 16

// helpTwoColMinW is the minimum overlay width for the two-column layout;
// below it, two columns would squeeze the action text into unreadable
// slivers, so the overlay falls back to a single column.
const helpTwoColMinW = 84

// HelpColumnsView renders the shared "?" overlay from titled sections. On
// wide terminals the sections flow into two balanced columns so a long key
// reference stays within the screen height instead of running off the bottom;
// narrow terminals keep the single column. Whole sections never split across
// columns, and action text soft-wraps inside its column with a hanging
// indent, so no content is ever clipped horizontally.
func HelpColumnsView(title string, sections []HelpSection, termW int) string {
	// The overlay takes ~70% of the terminal width — it is the full key
	// reference, and cramped columns wrap more than they inform. Narrow
	// terminals still get almost the whole width via the floor.
	w := termW * 7 / 10
	if w > termW-4 {
		w = termW - 4
	}
	if w < 30 {
		w = 30
	}
	// HelpView pads 2 cells on each side inside its width; column math must
	// target the inner content width or lipgloss re-wraps at the box edge.
	inner := w - 4

	if w < helpTwoColMinW {
		blocks := make([]string, 0, len(sections))
		for _, sec := range sections {
			blocks = append(blocks, renderHelpSection(sec, inner))
		}
		return HelpView(title, strings.Join(blocks, "\n\n"), w)
	}

	const gutter = 3
	colW := (inner - gutter) / 2
	blocks := make([]string, 0, len(sections))
	heights := make([]int, 0, len(sections))
	for _, sec := range sections {
		block := renderHelpSection(sec, colW)
		blocks = append(blocks, block)
		heights = append(heights, lipgloss.Height(block))
	}

	split := balanceSplit(heights)
	left := strings.Join(blocks[:split], "\n\n")
	right := strings.Join(blocks[split:], "\n\n")
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(colW).Render(left),
		strings.Repeat(" ", gutter),
		right,
	)
	return HelpView(title, body, w)
}

// balanceSplit picks the prefix split of section heights that minimizes the
// taller column — sections keep their reading order (left column top-to-
// bottom, then right), only the split point moves. Each gap between stacked
// sections costs one blank line.
func balanceSplit(heights []int) int {
	total := 0
	for _, h := range heights {
		total += h + 1
	}
	best, bestMax := len(heights), 1<<30
	leftH := 0
	for split := 1; split <= len(heights); split++ {
		leftH += heights[split-1] + 1
		rightH := total - leftH
		colMax := max(leftH, rightH)
		if colMax < bestMax {
			bestMax = colMax
			best = split
		}
	}
	return best
}

// renderHelpSection renders one titled block wrapped to colW display cells.
// Wrapped action lines hang-indent under the action column so keys stay
// scannable.
func renderHelpSection(sec HelpSection, colW int) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true).Width(helpKeyColW)
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())).Bold(true)

	actionW := colW - helpKeyColW
	if actionW < 20 {
		actionW = 20
	}
	indent := strings.Repeat(" ", helpKeyColW)

	var b strings.Builder
	b.WriteString(sectionStyle.Render(sec.Title) + "\n")
	for _, r := range sec.Rows {
		lines := softWrapCells(r.Action, actionW)
		b.WriteString(keyStyle.Render(r.Key) + lines[0] + "\n")
		for _, l := range lines[1:] {
			b.WriteString(indent + l + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// softWrapCells word-wraps text to a display-cell width (wide runes count as
// two cells), hard-breaking any single word wider than the column so nothing
// overflows.
func softWrapCells(s string, width int) []string {
	if runewidth.StringWidth(s) <= width {
		return []string{s}
	}
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		cand := word
		if cur != "" {
			cand = cur + " " + word
		}
		if runewidth.StringWidth(cand) <= width {
			cur = cand
			continue
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		for runewidth.StringWidth(word) > width {
			r := []rune(word)
			cut := len(r)
			for runewidth.StringWidth(string(r[:cut])) > width {
				cut--
			}
			lines = append(lines, string(r[:cut]))
			word = string(r[cut:])
		}
		cur = word
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
