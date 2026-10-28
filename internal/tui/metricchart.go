package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/ryandam9/aws_explorer/internal/brailleplot"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// metricChartRows is the height of the detail panel's metric chart.
const metricChartRows = 4

// metricChartMaxCols keeps the chart readable on wide terminals: past this,
// each datapoint just becomes a longer flat step.
const metricChartMaxCols = 72

// renderMetricChart draws vals as a braille area chart that fits a detail
// panel of the given width, indented by indent. It returns "" when there is
// nothing to draw. Gaps (NaN) stay blank.
func renderMetricChart(vals []float64, width, indent int) string {
	cols := min(width-indent, metricChartMaxCols)
	if cols < 1 {
		return ""
	}
	chart := brailleplot.Area(vals, cols, metricChartRows)
	if len(chart.Rows) == 0 {
		return ""
	}
	styles := map[brailleplot.Cat]lipgloss.Style{
		brailleplot.Grid: lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorBorder())),
		brailleplot.Fill: lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())),
		brailleplot.Edge: lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true),
	}
	lines := chart.Render(func(cat brailleplot.Cat, s string) string {
		if st, ok := styles[cat]; ok {
			return st.Render(s)
		}
		return s
	})
	pad := strings.Repeat(" ", indent)
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}
