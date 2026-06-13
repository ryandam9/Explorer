// Package sparkline renders a slice of numbers as a compact Unicode
// block-character sparkline for the TUI detail panels (AXE-014). It is a pure
// string transform — no AWS, no Bubble Tea — so it is exercised entirely with
// golden tests.
//
// The line scales between the data's own min and max (like the classic `spark`
// tool), so it shows the *shape* of the series; absolute level is conveyed
// separately by the "now"/"max" annotations the caller adds. A missing
// datapoint (NaN) renders as a blank gap rather than a misleading zero.
package sparkline

import (
	"math"
	"strconv"
	"strings"
)

// blocks are the eight ascending block characters U+2581…U+2588.
var blocks = []rune("▁▂▃▄▅▆▇█")

// gap is rendered for NaN (missing) datapoints.
const gap = ' '

// Render returns a sparkline for values. An empty series (or one with no
// real datapoints) returns "". NaN entries become blank gaps; the remaining
// points are scaled across the full block range between the series min and max.
func Render(values []float64) string {
	if len(values) == 0 {
		return ""
	}
	min, max, ok := minMax(values)
	if !ok {
		return "" // every datapoint was missing
	}
	var b strings.Builder
	b.Grow(len(values) * 3)
	span := max - min
	for _, v := range values {
		if math.IsNaN(v) {
			b.WriteRune(gap)
			continue
		}
		var level int
		if span > 0 {
			level = int(math.Round((v - min) / span * float64(len(blocks)-1)))
		} else {
			// Flat series: zeros sit on the floor, any other constant rides
			// mid-height so a steady non-zero line is still visible.
			if v != 0 {
				level = len(blocks) / 2
			}
		}
		if level < 0 {
			level = 0
		} else if level > len(blocks)-1 {
			level = len(blocks) - 1
		}
		b.WriteRune(blocks[level])
	}
	return b.String()
}

// Stats summarizes a series for the annotation the caller prints next to the
// sparkline. now is the most recent real datapoint; max is the largest. ok is
// false when there are no real datapoints at all.
type Stats struct {
	Now float64
	Max float64
	Min float64
}

// Summarize returns the latest and extreme values, ignoring NaN gaps.
func Summarize(values []float64) (Stats, bool) {
	min, max, ok := minMax(values)
	if !ok {
		return Stats{}, false
	}
	now := math.NaN()
	for i := len(values) - 1; i >= 0; i-- {
		if !math.IsNaN(values[i]) {
			now = values[i]
			break
		}
	}
	return Stats{Now: now, Max: max, Min: min}, true
}

// minMax returns the min and max of the non-NaN values, ok=false when none.
func minMax(values []float64) (min, max float64, ok bool) {
	for _, v := range values {
		if math.IsNaN(v) {
			continue
		}
		if !ok {
			min, max, ok = v, v, true
			continue
		}
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max, ok
}

// FormatValue renders a metric value compactly: integers print without a
// decimal point, fractional values keep up to two significant decimals, and
// large counts get k/M suffixes. unit is appended (e.g. "%") when non-empty.
func FormatValue(v float64, unit string) string {
	if math.IsNaN(v) {
		return "—"
	}
	var s string
	switch {
	case math.Abs(v) >= 1_000_000:
		s = strconv.FormatFloat(v/1_000_000, 'f', 1, 64) + "M"
	case math.Abs(v) >= 10_000:
		s = strconv.FormatFloat(v/1_000, 'f', 1, 64) + "k"
	case v == math.Trunc(v):
		s = strconv.FormatInt(int64(v), 10)
	default:
		s = strconv.FormatFloat(v, 'f', 2, 64)
	}
	if unit != "" {
		s += unit
	}
	return s
}
