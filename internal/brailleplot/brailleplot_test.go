package brailleplot

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPackBits(t *testing.T) {
	// One cell, every dot set one at a time, must give the standard braille
	// bit for that position.
	want := [2][4]rune{{'⠁', '⠂', '⠄', '⡀'}, {'⠈', '⠐', '⠠', '⢀'}}
	for i := 0; i < 2; i++ {
		for j := 0; j < 4; j++ {
			g := make([]Cat, 8)
			g[j*2+i] = Edge
			got := pack(g, 2, 4)[0][0]
			if got.Ch != want[i][j] || got.Cat != Edge {
				t.Errorf("dot (%d,%d) = %q cat %d, want %q", i, j, got.Ch, got.Cat, want[i][j])
			}
		}
	}
	// A cell takes the highest category of its dots; an empty cell is a space.
	g := []Cat{Grid, Fill, None, None, Edge, None, None, None}
	if got := pack(g, 2, 4)[0][0]; got.Cat != Edge {
		t.Errorf("mixed cell cat = %d, want Edge", got.Cat)
	}
	if got := pack(make([]Cat, 8), 2, 4)[0][0]; got.Ch != ' ' || got.Cat != None {
		t.Errorf("empty cell = %q/%d, want space/None", got.Ch, got.Cat)
	}
}

func TestAreaSizeAndShape(t *testing.T) {
	c := Area([]float64{1, 5, 3, 8, 2, 9, 4}, 20, 4)
	lines := c.Lines()
	if len(lines) != 4 {
		t.Fatalf("rows = %d, want 4", len(lines))
	}
	for i, l := range lines {
		if n := utf8.RuneCountInString(l); n != 20 {
			t.Errorf("row %d has %d cells, want 20: %q", i, n, l)
		}
	}
	// The bottom row is fully drawn (every value is above zero).
	if strings.Contains(lines[3], " ") {
		t.Errorf("bottom row has a blank cell: %q", lines[3])
	}
}

func TestAreaGolden(t *testing.T) {
	got := strings.Join(Area([]float64{0, 4, 8, 4, 0}, 8, 2).Lines(), "\n")
	// A peak in the middle: edge rising and falling, checkerboard fill
	// under it, dotted guides at the top every 4th dot column.
	want := "⠁ ⢁⣴⢧⡀⠁ \n" +
		"⣁⣴⢟⢕⢕⢝⢧⣀"
	if got != want {
		t.Errorf("chart =\n%s\nwant\n%s", got, want)
	}
}

func TestAreaGapIsBlank(t *testing.T) {
	nan := math.NaN()
	// 4 values over 4 cells = 8 dot columns: the middle values are missing.
	c := Area([]float64{5, nan, nan, 5}, 4, 2)
	for r, row := range c.Rows {
		for x := 1; x <= 2; x++ {
			if row[x].Ch != ' ' {
				t.Errorf("row %d cell %d = %q, want blank for missing data", r, x, row[x].Ch)
			}
		}
	}
}

func TestAreaZeroIsNotAGap(t *testing.T) {
	c := Area([]float64{0, 0, 0}, 3, 2)
	bottom := c.Rows[len(c.Rows)-1]
	for i, g := range bottom {
		if g.Cat != Edge {
			t.Errorf("zero value cell %d cat = %d, want Edge on the floor", i, g.Cat)
		}
	}
	if top := c.Lines()[0]; strings.ContainsAny(top, "⣀⣤⣶⣿") {
		t.Errorf("all-zero series rose above the floor: %q", top)
	}
}

func TestAreaDownsampleKeepsPeak(t *testing.T) {
	// 100 points into 2 dot columns (1 cell): one spike must reach the top.
	vals := make([]float64, 100)
	vals[37] = 10
	vals[99] = 1
	c := Area(vals, 1, 2)
	if c.Rows[0][0].Cat != Edge {
		t.Errorf("spike lost when downsampling: top cell = %q", c.Rows[0][0].Ch)
	}
}

func TestAreaDegenerate(t *testing.T) {
	nan := math.NaN()
	for _, tc := range []struct {
		vals       []float64
		cols, rows int
	}{
		{nil, 10, 3},
		{[]float64{nan, nan}, 10, 3},
		{[]float64{1, 2}, 0, 3},
		{[]float64{1, 2}, 10, 0},
		{[]float64{1, 2}, -1, -1},
	} {
		if c := Area(tc.vals, tc.cols, tc.rows); len(c.Rows) != 0 {
			t.Errorf("Area(%v, %d, %d) = %d rows, want empty", tc.vals, tc.cols, tc.rows, len(c.Rows))
		}
	}
	// A single value and a negative series render without panicking.
	if c := Area([]float64{7}, 5, 2); len(c.Rows) != 2 {
		t.Errorf("single value rows = %d", len(c.Rows))
	}
	if c := Area([]float64{-3, -1, -2}, 5, 2); len(c.Rows) != 2 {
		t.Errorf("negative series rows = %d", len(c.Rows))
	}
}

func TestRenderGroupsRuns(t *testing.T) {
	c := Chart{Rows: [][]Glyph{{{'a', Fill}, {'b', Fill}, {'c', Edge}, {' ', None}}}}
	calls := 0
	got := c.Render(func(cat Cat, s string) string {
		calls++
		return "[" + s + "]"
	})
	if got[0] != "[ab][c][ ]" || calls != 3 {
		t.Errorf("Render = %q (%d calls), want [ab][c][ ] in 3 calls", got[0], calls)
	}
}
