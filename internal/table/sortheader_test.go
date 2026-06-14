package table

import "testing"

func TestApplySortHeader_ArrowOnActiveOnly(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: 20},
		{Title: "Region", Width: 12},
	}
	sortable := func(i int) bool { return i > 0 }
	ApplySortHeader(cols, 1, true, sortable)

	if cols[0].Title != "#" {
		t.Errorf("non-sortable column got an arrow: %q", cols[0].Title)
	}
	if cols[1].Title != "Name"+SortAscArrow {
		t.Errorf("active column title = %q, want %q", cols[1].Title, "Name"+SortAscArrow)
	}
	if cols[2].Title != "Region" {
		t.Errorf("inactive column got an arrow: %q", cols[2].Title)
	}
}

// The defining property: a column's width must not change when the sort moves
// onto, off, or across it — otherwise the table reflows ("flickers"). We assert
// the fitted widths are identical across every sort state.
func TestApplySortHeader_WidthStableAcrossSortStates(t *testing.T) {
	base := []Column{
		{Title: "#", Width: 4},
		{Title: "Service", Width: 8}, // narrower than title+arrow, so it must grow
		{Title: "Region", Width: 12},
		{Title: "Name", Width: 20},
	}
	sortable := func(i int) bool { return i > 0 }

	fresh := func() []Column {
		c := make([]Column, len(base))
		copy(c, base)
		return c
	}

	// Width of column 1 with the sort inactive on it...
	a := fresh()
	ApplySortHeader(a, 2, true, sortable) // sort on column 2, not 1
	// ...must equal its width when the sort is active on it, ascending...
	b := fresh()
	ApplySortHeader(b, 1, true, sortable)
	// ...and descending.
	c := fresh()
	ApplySortHeader(c, 1, false, sortable)

	for i := range base {
		if a[i].Width != b[i].Width || b[i].Width != c[i].Width {
			t.Errorf("column %d width not stable across sort states: inactive=%d asc=%d desc=%d",
				i, a[i].Width, b[i].Width, c[i].Width)
		}
	}
	// And the reserved column actually grew to fit title+arrow.
	if b[1].Width < len("Service")+2 {
		t.Errorf("active column width %d did not reserve room for the arrow", b[1].Width)
	}
}

func TestApplySortHeader_NoneActive(t *testing.T) {
	cols := []Column{{Title: "#", Width: 4}, {Title: "Name", Width: 20}}
	ApplySortHeader(cols, -1, true, func(i int) bool { return i > 0 })
	if cols[1].Title != "Name" {
		t.Errorf("no column should carry an arrow when active=-1, got %q", cols[1].Title)
	}
}
