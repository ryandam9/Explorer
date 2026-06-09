package table

import "testing"

func colTitles(m Model) []string {
	var out []string
	for _, i := range m.visibleCols() {
		out = append(out, m.cols[i].Title)
	}
	return out
}

func TestColumnScrolling(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "A", Width: 20},
		{Title: "B", Width: 20},
		{Title: "C", Width: 20},
		{Title: "D", Width: 20},
	}
	// Spans (incl. padding 2): # =6, A/B/C/D =22 each.
	m := New(WithColumns(cols), WithRows([]Row{{"1", "a", "b", "c", "d"}}))
	m.SetWidth(50) // fits # + A + B

	assertEq := func(label string, got, want []string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s: got %v, want %v", label, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%s: got %v, want %v", label, got, want)
			}
		}
	}

	assertEq("offset0", colTitles(m), []string{"#", "A", "B"})
	if l, r := m.ColScrollInfo(); l != 0 || r != 2 {
		t.Fatalf("offset0 scroll info: got (%d,%d), want (0,2)", l, r)
	}

	m.ScrollRight()
	assertEq("offset1", colTitles(m), []string{"#", "B", "C"})
	if l, r := m.ColScrollInfo(); l != 1 || r != 1 {
		t.Fatalf("offset1 scroll info: got (%d,%d), want (1,1)", l, r)
	}

	m.ScrollRight()
	assertEq("offset2", colTitles(m), []string{"#", "C", "D"})
	if l, r := m.ColScrollInfo(); l != 2 || r != 0 {
		t.Fatalf("offset2 scroll info: got (%d,%d), want (2,0)", l, r)
	}

	// Already at the rightmost: further scroll is a no-op.
	m.ScrollRight()
	assertEq("clamped right", colTitles(m), []string{"#", "C", "D"})

	// Scroll all the way back.
	m.ScrollLeft()
	m.ScrollLeft()
	m.ScrollLeft() // extra is a no-op
	assertEq("back to offset0", colTitles(m), []string{"#", "A", "B"})

	// The frozen column is present at every offset.
	for off := 0; off <= 2; off++ {
		titles := colTitles(m)
		if titles[0] != "#" {
			t.Fatalf("offset %d: frozen column missing, got %v", off, titles)
		}
		m.ScrollRight()
	}

	// Unconstrained width shows every column.
	m.SetWidth(0)
	assertEq("unconstrained", colTitles(m), []string{"#", "A", "B", "C", "D"})
}
