// Package brailleplot draws a numeric series as a multi-row area chart made of
// braille characters. Each terminal cell holds a 2×4 grid of dots, so a chart
// has twice the horizontal and four times the vertical resolution of the
// one-row block sparkline in internal/sparkline.
//
// It is a pure transform (no AWS, no Bubble Tea, no lipgloss) so it is tested
// with fixtures. Styling is left to the caller: every cell carries a category
// (grid, fill, edge) and Render hands each same-category run to a paint func.
//
// The approach is adapted from velo (github.com/bjarneo/velo, MIT), whose
// pack() turns a categorised dot grid into braille glyphs.
package brailleplot

import (
	"math"
	"strings"
)

// Cat is the category of a cell. It picks the cell's color. A cell takes the
// highest category of the dots inside it.
type Cat uint8

const (
	None Cat = iota // empty cell
	Grid            // dotted guide lines
	Fill            // the area under the line
	Edge            // the line itself
)

// Glyph is one rendered cell.
type Glyph struct {
	Ch  rune
	Cat Cat
}

// Chart is a rendered chart: Rows[0] is the top row.
type Chart struct {
	Rows [][]Glyph
}

// brailleBit maps the dot at column i, row j of a cell to its bit.
var brailleBit = [2][4]uint8{{0x01, 0x02, 0x04, 0x40}, {0x08, 0x10, 0x20, 0x80}}

// pack turns a w×h dot grid of categories (row-major, top row first) into
// rows of braille glyphs. w must be a multiple of 2 and h of 4. An empty cell
// becomes a space, not the blank braille glyph U+2800, which some fonts draw
// visibly.
func pack(g []Cat, w, h int) [][]Glyph {
	cols, rows := w/2, h/4
	out := make([][]Glyph, rows)
	for r := 0; r < rows; r++ {
		row := make([]Glyph, cols)
		for c := 0; c < cols; c++ {
			var bits uint8
			var cat Cat
			for i := 0; i < 2; i++ {
				for j := 0; j < 4; j++ {
					if k := g[(r*4+j)*w+c*2+i]; k != None {
						bits |= brailleBit[i][j]
						cat = max(cat, k)
					}
				}
			}
			ch := ' '
			if bits != 0 {
				ch = rune(0x2800 + int(bits))
			}
			row[c] = Glyph{ch, cat}
		}
		out[r] = row
	}
	return out
}

// Area draws values as a filled area chart cols cells wide and rows cells
// tall. The y axis runs from min(0, data min) to the data max, so the area
// reads as magnitude. The caller prints the scale (e.g. "max 42").
//
// A NaN value is missing data. Its columns stay blank, so a gap never looks
// like zero. A real zero still lights the bottom dot row. When the series has
// more points than the chart has dot columns, each column shows the peak of
// its points, so short spikes stay visible. Area returns an empty Chart when
// the size is too small or no value is real.
func Area(values []float64, cols, rows int) Chart {
	if cols <= 0 || rows <= 0 || len(values) == 0 {
		return Chart{}
	}
	lo, hi, ok := minMax(values)
	if !ok {
		return Chart{}
	}
	lo = math.Min(lo, 0)
	if hi == lo {
		// Only an all-zero (or constant negative) series gets here, because
		// lo includes 0. It sits on the floor.
		hi = lo + 1
	}

	w, h := cols*2, rows*4
	col := resample(values, w)

	// top[x] is the dot height of column x (1..h), or 0 for a gap.
	top := make([]int, w)
	for x, v := range col {
		if math.IsNaN(v) {
			continue
		}
		t := int(math.Round((v - lo) / (hi - lo) * float64(h)))
		top[x] = min(h, max(1, t))
	}

	g := make([]Cat, w*h)
	for x := 0; x < w; x++ {
		if top[x] == 0 {
			continue // missing data: leave the column blank
		}
		// The edge is two dots thick. Where the neighbour is far away, it
		// extends half way towards it so steep slopes stay connected.
		edgeLo := top[x] - 1
		for _, n := range [2]int{x - 1, x + 1} {
			if n >= 0 && n < w && top[n] != 0 && top[n] < edgeLo {
				edgeLo = min(edgeLo, (top[x]+top[n]+1)/2)
			}
		}
		for yb := 1; yb <= h; yb++ { // yb counts dot rows from the bottom
			var k Cat
			switch {
			case yb > top[x]:
				// Above the line: dotted guides at the top and middle.
				if (yb == h || yb == h/2) && x%4 == 0 {
					k = Grid
				}
			case yb >= edgeLo:
				k = Edge
			case (x+yb)%2 == 0:
				k = Fill
			}
			g[(h-yb)*w+x] = k
		}
	}
	return Chart{Rows: pack(g, w, h)}
}

// resample maps values onto n dot columns. With fewer values than columns it
// interpolates linearly between neighbours (a NaN neighbour makes the column
// a gap). With more, each column takes the peak of its bucket.
func resample(values []float64, n int) []float64 {
	out := make([]float64, n)
	m := len(values)
	if m == 1 {
		for i := range out {
			out[i] = values[0]
		}
		return out
	}
	if m > n {
		for x := 0; x < n; x++ {
			a, b := x*m/n, (x+1)*m/n
			peak := math.NaN()
			for _, v := range values[a:b] {
				if !math.IsNaN(v) && (math.IsNaN(peak) || v > peak) {
					peak = v
				}
			}
			out[x] = peak
		}
		return out
	}
	for x := 0; x < n; x++ {
		pos := 0.0
		if n > 1 {
			pos = float64(x) / float64(n-1) * float64(m-1)
		}
		i := int(pos)
		if i >= m-1 {
			out[x] = values[m-1]
			continue
		}
		f := pos - float64(i)
		a, b := values[i], values[i+1]
		switch {
		case f < 1e-9:
			out[x] = a
		case math.IsNaN(a) || math.IsNaN(b):
			out[x] = math.NaN()
		default:
			out[x] = a + (b-a)*f
		}
	}
	return out
}

func minMax(values []float64) (lo, hi float64, ok bool) {
	for _, v := range values {
		if math.IsNaN(v) {
			continue
		}
		if !ok {
			lo, hi, ok = v, v, true
			continue
		}
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi, ok
}

// Lines returns the chart as plain text, one string per row.
func (c Chart) Lines() []string {
	return c.Render(func(_ Cat, s string) string { return s })
}

// Render returns one string per row. paint is called once per run of cells
// that share a category and returns the styled run.
func (c Chart) Render(paint func(cat Cat, s string) string) []string {
	out := make([]string, len(c.Rows))
	for r, row := range c.Rows {
		var b, run strings.Builder
		cur := None
		flush := func() {
			if run.Len() > 0 {
				b.WriteString(paint(cur, run.String()))
				run.Reset()
			}
		}
		for i, g := range row {
			if i > 0 && g.Cat != cur {
				flush()
			}
			cur = g.Cat
			run.WriteRune(g.Ch)
		}
		flush()
		out[r] = b.String()
	}
	return out
}
