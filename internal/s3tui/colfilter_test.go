package s3tui

import (
	"strings"
	"testing"
)

// csv with two populated columns (a, c) and two entirely-empty columns (b, d).
const colFilterCSV = "a,b,c,d\n" +
	"1, ,3,\n" +
	"4,,6, \n"

func TestColHasData(t *testing.T) {
	data := [][]string{{"1", " ", "3", ""}, {"4", "", "6", " "}}
	for col, want := range map[int]bool{0: true, 1: false, 2: true, 3: false} {
		if got := colHasData(data, col); got != want {
			t.Errorf("colHasData(col %d) = %v, want %v", col, got, want)
		}
	}
	// nil divider rows are ignored, not treated as data.
	if colHasData([][]string{nil}, 0) {
		t.Error("a nil divider row should not count as data")
	}
}

func TestFilterColIndices(t *testing.T) {
	header := []string{"a", "b", "c", "d"}
	data := [][]string{{"1", "", "3", ""}, {"4", "", "6", ""}}

	if got := filterColIndices(header, data, colFilterAll, false); !eqInts(got, []int{0, 1, 2, 3}) {
		t.Errorf("all = %v", got)
	}
	if got := filterColIndices(header, data, colFilterWithData, false); !eqInts(got, []int{0, 2}) {
		t.Errorf("with-data = %v, want [0 2]", got)
	}
	if got := filterColIndices(header, data, colFilterEmpty, false); !eqInts(got, []int{1, 3}) {
		t.Errorf("empty = %v, want [1 3]", got)
	}

	// Fixed-width: the leading marker column (index 0) is always kept.
	fh := []string{"!", "a", "b"}
	fd := [][]string{{"!", "1", ""}}
	if got := filterColIndices(fh, fd, colFilterWithData, true); !eqInts(got, []int{0, 1}) {
		t.Errorf("fixed with-data = %v, want [0 1] (marker kept)", got)
	}
	if got := filterColIndices(fh, fd, colFilterEmpty, true); !eqInts(got, []int{0, 2}) {
		t.Errorf("fixed empty = %v, want [0 2] (marker kept)", got)
	}
}

// Cycling the filter moves all → with-data → empty → all and rebuilds the table
// with the surviving columns, keeping each column's original number.
func TestCycleCSVColFilter(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV(colFilterCSV) {
		t.Fatal("initCSV should parse")
	}
	if m.csvColFilter != colFilterAll {
		t.Fatalf("default filter = %v, want all", m.csvColFilter)
	}

	m.cycleCSVColFilter() // → with data
	if m.csvColFilter != colFilterWithData {
		t.Fatalf("after 1st cycle = %v, want with-data", m.csvColFilter)
	}
	if !eqInts(m.csvVisCols, []int{0, 2}) {
		t.Errorf("with-data visible cols = %v, want [0 2]", m.csvVisCols)
	}
	view := m.csvTable.View()
	// Survivors keep their original numbers: a→(1), c→(3); b/d are gone.
	for _, want := range []string{"a", "c", "(1)", "(3)"} {
		if !strings.Contains(view, want) {
			t.Errorf("with-data table missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "(2)") || strings.Contains(view, "(4)") {
		t.Errorf("with-data table should not renumber survivors:\n%s", view)
	}

	m.cycleCSVColFilter() // → empty
	if m.csvColFilter != colFilterEmpty {
		t.Fatalf("after 2nd cycle = %v, want empty", m.csvColFilter)
	}
	if !eqInts(m.csvVisCols, []int{1, 3}) {
		t.Errorf("empty visible cols = %v, want [1 3]", m.csvVisCols)
	}

	m.cycleCSVColFilter() // → all
	if m.csvColFilter != colFilterAll {
		t.Fatalf("after 3rd cycle = %v, want all", m.csvColFilter)
	}
}

// When there are no empty columns, the "empty" mode is skipped (never a blank
// table) and a note explains why.
func TestCycleCSVColFilterSkipsEmptyMode(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("a,b\n1,2\n3,4\n") { // every column has data
		t.Fatal("initCSV should parse")
	}
	m.cycleCSVColFilter() // → with-data (all columns)
	if m.csvColFilter != colFilterWithData {
		t.Fatalf("filter = %v, want with-data", m.csvColFilter)
	}
	m.cycleCSVColFilter() // "empty" has no columns → skipped, back to all
	if m.csvColFilter != colFilterAll {
		t.Fatalf("filter = %v, want all (empty skipped)", m.csvColFilter)
	}
	if !strings.Contains(m.csvNote, "no empty columns") {
		t.Errorf("expected a skip note, got %q", m.csvNote)
	}
}

// The info line reports the filter and the kept/total counts.
func TestColFilterInfoLine(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV(colFilterCSV) {
		t.Fatal("initCSV should parse")
	}
	m.cycleCSVColFilter() // with data
	if got := m.csvInfoLine(); !strings.Contains(got, "2 of 4 columns with data") {
		t.Errorf("info line = %q, want it to mention '2 of 4 columns with data'", got)
	}
}

// The vertical record view shows only the filtered columns, keeping their
// original column numbers.
func TestColFilterRecordView(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("id,mid,name,ext\n1,,alice,\n2,,bob,\n") {
		t.Fatal("initCSV should parse")
	}
	m.cycleCSVColFilter() // with data → keeps id(1) and name(3)
	m.csvTable.SetCursor(0)
	m.openCSVRecord()
	view := m.csvRecordViewport.View()
	for _, want := range []string{"1: id", "3: name", "alice"} {
		if !strings.Contains(view, want) {
			t.Errorf("record view missing %q:\n%s", want, view)
		}
	}
	for _, absent := range []string{"mid", "ext"} {
		if strings.Contains(view, absent) {
			t.Errorf("record view should hide empty column %q:\n%s", absent, view)
		}
	}
}

func TestProjectCols(t *testing.T) {
	header := []string{"a", "b", "c"}
	rows := [][]string{{"1", "2", "3"}, nil, {"4", "5", "6"}}
	h, out := projectCols(header, rows, []int{0, 2})
	if !eqStrings(h, []string{"a", "c"}) {
		t.Errorf("header = %v, want [a c]", h)
	}
	if out[1] != nil {
		t.Errorf("divider row should stay nil, got %v", out[1])
	}
	if !eqStrings(out[0], []string{"1", "3"}) || !eqStrings(out[2], []string{"4", "6"}) {
		t.Errorf("projected rows = %v", out)
	}
}

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
