package tui

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderMetricChartFitsPanel(t *testing.T) {
	vals := []float64{1, 3, math.NaN(), 7, 4, 9, 2, 5, 6, 8, 3, 1}
	for _, width := range []int{3, 10, 40, 80, 200} {
		out := renderMetricChart(vals, width, 2)
		lines := strings.Split(out, "\n")
		if len(lines) != metricChartRows {
			t.Fatalf("width %d: %d rows, want %d", width, len(lines), metricChartRows)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w > width || w > metricChartMaxCols+2 {
				t.Errorf("width %d row %d is %d cells wide", width, i, w)
			}
			if !strings.HasPrefix(ansi.Strip(l), "  ") {
				t.Errorf("width %d row %d lost its indent: %q", width, i, ansi.Strip(l))
			}
		}
	}
}

func TestRenderMetricChartEmpty(t *testing.T) {
	if out := renderMetricChart([]float64{math.NaN()}, 80, 2); out != "" {
		t.Errorf("all-missing series = %q, want empty", out)
	}
	if out := renderMetricChart([]float64{1, 2}, 2, 2); out != "" {
		t.Errorf("no room = %q, want empty", out)
	}
}
