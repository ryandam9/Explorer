package table

import (
	"strings"
	"testing"
)

func TestColNumbersAddSecondHeaderLine(t *testing.T) {
	cols := []Column{
		{Title: "id", Width: 6},
		{Title: "name", Width: 10},
		{Title: "city", Width: 10},
	}
	m := New(WithColumns(cols), WithRows([]Row{{"1", "ann", "rome"}}), WithColNumbers(true))
	m.SetWidth(60) // wide enough to show every column

	hdr := m.headersView()
	lines := strings.Split(hdr, "\n")
	if len(lines) != 2 {
		t.Fatalf("headersView lines = %d, want 2:\n%s", len(lines), hdr)
	}
	// Titles on the first line, numbers on the second.
	if !strings.Contains(lines[0], "id") || !strings.Contains(lines[0], "name") {
		t.Errorf("first header line missing titles: %q", lines[0])
	}
	for _, n := range []string{"(1)", "(2)", "(3)"} {
		if !strings.Contains(lines[1], n) {
			t.Errorf("number line missing %s: %q", n, lines[1])
		}
	}
}

func TestColNumbersOffByDefault(t *testing.T) {
	m := New(WithColumns([]Column{{Title: "a", Width: 4}, {Title: "b", Width: 4}}))
	m.SetWidth(40)
	if strings.Contains(m.headersView(), "\n") {
		t.Errorf("default header should be a single line: %q", m.headersView())
	}
}

// The number row must show the absolute column index, so a scrolled-away
// column keeps its original number rather than being renumbered from 1.
func TestColNumbersUseAbsoluteIndexWhenScrolled(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "A", Width: 20},
		{Title: "B", Width: 20},
		{Title: "C", Width: 20},
		{Title: "D", Width: 20},
	}
	m := New(WithColumns(cols), WithRows([]Row{{"1", "a", "b", "c", "d"}}), WithColNumbers(true))
	m.SetWidth(50)
	m.ScrollRight() // frozen "#" + B + C now visible

	lines := strings.Split(m.headersView(), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 header lines, got %d", len(lines))
	}
	// Frozen column keeps (1); the scrolled-in columns keep (3) and (4).
	for _, n := range []string{"(1)", "(3)", "(4)"} {
		if !strings.Contains(lines[1], n) {
			t.Errorf("scrolled number line missing %s: %q", n, lines[1])
		}
	}
	if strings.Contains(lines[1], "(2)") {
		t.Errorf("column 2 is scrolled off but (2) still shown: %q", lines[1])
	}
}

// A NoNumber column (e.g. a leading "!" marker) must not get an ordinal, and
// the first real data column must be numbered (1), not (2).
func TestColNumbersSkipNoNumberColumn(t *testing.T) {
	cols := []Column{
		{Title: "!", Width: 4, NoNumber: true},
		{Title: "id", Width: 6},
		{Title: "name", Width: 10},
		{Title: "amt", Width: 8},
	}
	m := New(WithColumns(cols), WithRows([]Row{{"", "1", "ann", "10"}}), WithColNumbers(true))
	m.SetWidth(60) // wide enough to show every column

	lines := strings.Split(m.headersView(), "\n")
	if len(lines) != 2 {
		t.Fatalf("headersView lines = %d, want 2:\n%s", len(lines), m.headersView())
	}
	// The three real columns are numbered 1..3; there is no (4).
	for _, n := range []string{"(1)", "(2)", "(3)"} {
		if !strings.Contains(lines[1], n) {
			t.Errorf("number line missing %s: %q", n, lines[1])
		}
	}
	if strings.Contains(lines[1], "(4)") {
		t.Errorf("marker column should not produce a 4th ordinal: %q", lines[1])
	}
}

func TestVisibleScrollableCols(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "A", Width: 20},
		{Title: "B", Width: 20},
		{Title: "C", Width: 20},
		{Title: "D", Width: 20},
	}
	m := New(WithColumns(cols), WithRows([]Row{{"1", "a", "b", "c", "d"}}))
	m.SetWidth(50) // frozen # + A + B

	lo, hi, ok := m.VisibleScrollableCols()
	if !ok || lo != 2 || hi != 3 {
		t.Fatalf("offset0: got (%d,%d,%v), want (2,3,true)", lo, hi, ok)
	}

	m.ScrollRight() // # + B + C
	lo, hi, ok = m.VisibleScrollableCols()
	if !ok || lo != 3 || hi != 4 {
		t.Fatalf("offset1: got (%d,%d,%v), want (3,4,true)", lo, hi, ok)
	}
}

// The body viewport must shrink by exactly the extra header line so the table
// still fits its allotted height.
func TestColNumbersShrinkViewportByOneLine(t *testing.T) {
	cols := []Column{{Title: "a", Width: 6}, {Title: "b", Width: 6}}
	rows := []Row{{"1", "2"}, {"3", "4"}}

	plain := New(WithColumns(cols), WithRows(rows))
	plain.SetWidth(40)
	plain.SetHeight(12)

	numbered := New(WithColumns(cols), WithRows(rows), WithColNumbers(true))
	numbered.SetWidth(40)
	numbered.SetHeight(12)

	if got := plain.Height() - numbered.Height(); got != 1 {
		t.Errorf("numbered table body height differs by %d, want 1", got)
	}
}
