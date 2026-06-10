package table

import (
	"strings"
	"testing"
)

// colWidth returns the on-screen width of the named column after auto-fit.
func colWidth(m Model, title string) int {
	for _, c := range m.cols {
		if c.Title == title {
			return c.Width
		}
	}
	return -1
}

// TestFitColumnsGrowsToContent verifies a column whose value is wider than its
// configured width grows to fit, so the value is rendered in full rather than
// truncated with an ellipsis.
func TestFitColumnsGrowsToContent(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: 18},
	}
	long := "my-very-long-subnet-name-that-exceeds-eighteen"
	m := New(WithColumns(cols), WithRows([]Row{{"1", long}}))

	if got := colWidth(m, "Name"); got < len([]rune(long)) {
		t.Fatalf("Name width = %d, want >= %d to fit %q", got, len([]rune(long)), long)
	}

	view := m.View()
	if strings.Contains(view, "…") {
		t.Fatalf("view contains ellipsis, value was truncated:\n%s", view)
	}
	if !strings.Contains(view, long) {
		t.Fatalf("view does not contain full value %q:\n%s", long, view)
	}
}

// TestFitColumnsKeepsConfiguredFloor verifies columns never shrink below their
// configured width when content is short, and that width-0 columns stay hidden.
func TestFitColumnsKeepsConfiguredFloor(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: 18},
		{Title: "Tags", Width: 0}, // hidden / detail-only
	}
	m := New(WithColumns(cols), WithRows([]Row{{"1", "short", "a=b"}}))

	if got := colWidth(m, "Name"); got != 18 {
		t.Fatalf("Name width = %d, want 18 (configured floor)", got)
	}
	if got := colWidth(m, "Tags"); got != 0 {
		t.Fatalf("Tags width = %d, want 0 (stays hidden)", got)
	}
	for _, title := range colTitles(m) {
		if title == "Tags" {
			t.Fatalf("width-0 column became visible: %v", colTitles(m))
		}
	}
}

// TestFitColumnsRefitsOnNewRows verifies that replacing rows re-fits against the
// new content rather than retaining a previously grown width.
func TestFitColumnsRefitsOnNewRows(t *testing.T) {
	cols := []Column{
		{Title: "#", Width: 4},
		{Title: "Name", Width: 10},
	}
	m := New(WithColumns(cols))

	m.SetRows([]Row{{"1", "a-fairly-long-value-here"}})
	grown := colWidth(m, "Name")
	if grown < len("a-fairly-long-value-here") {
		t.Fatalf("Name did not grow: width = %d", grown)
	}

	// Shorter content should shrink back to the configured floor.
	m.SetRows([]Row{{"1", "x"}})
	if got := colWidth(m, "Name"); got != 10 {
		t.Fatalf("Name width = %d after shrink, want 10 (configured floor)", got)
	}
}
