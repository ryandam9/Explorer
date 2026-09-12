package table

import "testing"

// A value too tall for one line spans several rows but must behave as one:
// the cursor steps group by group and reports the logical row.
func TestRowGroupsNavigation(t *testing.T) {
	// Three logical rows: the first wraps to 3 lines, the second to 2, the
	// third to 1.
	groups := []int{0, 0, 0, 1, 1, 2}
	rows := make([]Row, len(groups))
	for i := range rows {
		rows[i] = Row{"cell"}
	}
	m := New(
		WithColumns([]Column{{Title: "Message", Width: 10}}),
		WithRows(rows),
		WithRowGroups(groups),
		WithHeight(10),
	)

	if got := m.CursorGroup(); got != 0 {
		t.Fatalf("initial CursorGroup = %d, want 0", got)
	}

	m.MoveDown(1)
	if got := m.CursorGroup(); got != 1 {
		t.Errorf("after one step down, group = %d, want 1 (not a wrapped line)", got)
	}
	if got := m.Cursor(); got != 3 {
		t.Errorf("cursor row = %d, want 3 — the first row of group 1", got)
	}

	m.MoveDown(1)
	if got := m.CursorGroup(); got != 2 {
		t.Errorf("second step down, group = %d, want 2", got)
	}

	// Past the end it stays on the last group rather than wrapping or hanging.
	m.MoveDown(5)
	if got := m.CursorGroup(); got != 2 {
		t.Errorf("stepping past the end, group = %d, want 2", got)
	}

	m.MoveUp(2)
	if got := m.CursorGroup(); got != 0 {
		t.Errorf("back to the top, group = %d, want 0", got)
	}
	if got := m.Cursor(); got != 0 {
		t.Errorf("cursor row = %d, want 0", got)
	}

	// SetCursorGroup lands on a group's first row, so a wrapped value is
	// entered from its top line.
	m.SetCursorGroup(1)
	if got := m.Cursor(); got != 3 {
		t.Errorf("SetCursorGroup(1) put the cursor on row %d, want 3", got)
	}

	// A row in the middle of a group resolves to that group's first row.
	m.SetCursor(2)
	if got := m.Cursor(); got != 0 {
		t.Errorf("SetCursor(2) inside group 0 = row %d, want its first row 0", got)
	}
}

// Without row groups every row is its own group — the behavior every other
// table in the app relies on.
func TestRowGroupsAbsentIsPlainRows(t *testing.T) {
	m := New(
		WithColumns([]Column{{Title: "A", Width: 4}}),
		WithRows([]Row{{"1"}, {"2"}, {"3"}}),
		WithHeight(5),
	)
	m.MoveDown(2)
	if got := m.Cursor(); got != 2 {
		t.Errorf("cursor = %d, want 2", got)
	}
	if got := m.CursorGroup(); got != m.Cursor() {
		t.Errorf("CursorGroup = %d, want it to equal Cursor %d", got, m.Cursor())
	}
}

// Selection and striping follow the group, so a wrapped value renders as one
// band instead of striping mid-value.
func TestRowGroupsShareSelectionAndStripe(t *testing.T) {
	groups := []int{0, 0, 1}
	m := New(
		WithColumns([]Column{{Title: "A", Width: 4}}),
		WithRows([]Row{{"a"}, {"a2"}, {"b"}}),
		WithRowGroups(groups),
		WithHeight(5),
	)
	if !m.sameGroup(0, 1) {
		t.Errorf("rows 0 and 1 are the same logical row but sameGroup said no")
	}
	if m.sameGroup(1, 2) {
		t.Errorf("rows 1 and 2 are different logical rows but sameGroup said yes")
	}
	if got := m.groupOf(1); got != 0 {
		t.Errorf("groupOf(1) = %d, want 0 — the stripe follows the value", got)
	}
}
