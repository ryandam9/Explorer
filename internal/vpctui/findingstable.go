package vpctui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// ---------------------------------------------------------------------------
// Findings table
//
// The findings overlay renders as an aligned table — severity, impacted
// resource, the issue (title plus why it fired), and the suggested fix — with
// every cell wrapped to its column so long sentences stay readable.
// ---------------------------------------------------------------------------

func severityGlyphLabel(s Severity) string {
	switch s {
	case SevCritical:
		return "🔴 CRITICAL"
	case SevWarning:
		return "🟡 WARNING"
	default:
		return "🔵 INFO"
	}
}

func severityStyle(s Severity) lipgloss.Style {
	switch s {
	case SevCritical:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorError()))
	case SevWarning:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorWarning()))
	default:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorInfo()))
	}
}

// buildFindingsTable renders findings as a four-column table sized to width.
func buildFindingsTable(findings []Finding, width int) string {
	if width < 60 {
		width = 60
	}
	const gap = 2
	sevW, resW := 11, 24
	rest := width - sevW - resW - 3*gap
	issueW := rest * 55 / 100
	fixW := rest - issueW

	widths := []int{sevW, resW, issueW, fixW}
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading()))
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))

	var b strings.Builder
	b.WriteString(joinCells(
		[]string{"SEVERITY", "RESOURCE", "ISSUE", "FIX"},
		widths, gap,
		[]lipgloss.Style{headerStyle, headerStyle, headerStyle, headerStyle}))
	b.WriteString("\n" + mutedStyle.Render(strings.Repeat("─", sevW+resW+issueW+fixW+3*gap)))

	for _, f := range findings {
		issue := f.Title
		if f.Detail != "" {
			issue += "\n" + f.Detail
		}
		b.WriteString("\n" + joinCells(
			[]string{severityGlyphLabel(f.Severity), f.Resource, issue, f.Fix},
			widths, gap,
			[]lipgloss.Style{severityStyle(f.Severity), mutedStyle, textStyle, mutedStyle}))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// joinCells lays raw cell texts out side by side: each cell is wrapped to its
// column width, short columns are padded to the tallest one, and the column's
// style is applied per line.
func joinCells(cells []string, widths []int, gap int, styles []lipgloss.Style) string {
	cols := make([][]string, len(cells))
	height := 1
	for i, c := range cells {
		cols[i] = strings.Split(lipgloss.NewStyle().Width(widths[i]).Render(c), "\n")
		if len(cols[i]) > height {
			height = len(cols[i])
		}
	}

	pad := strings.Repeat(" ", gap)
	var b strings.Builder
	for ln := 0; ln < height; ln++ {
		if ln > 0 {
			b.WriteString("\n")
		}
		for i := range cols {
			cell := ""
			if ln < len(cols[i]) {
				cell = cols[i][ln]
			}
			if fill := widths[i] - lipgloss.Width(cell); fill > 0 {
				cell += strings.Repeat(" ", fill)
			}
			if i > 0 {
				b.WriteString(pad)
			}
			b.WriteString(styles[i].Render(cell))
		}
	}
	return b.String()
}
