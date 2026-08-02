package table

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// A column Note renders after the ordinal on the numbering line, and the
// column is widened so the annotation is never clipped.
func TestColumnNoteOnNumberLine(t *testing.T) {
	m := New(
		WithColumns([]Column{
			{Title: "!", Width: 1, NoNumber: true},
			{Title: "id", Width: 2, Note: "1-10"},
			{Title: "name", Width: 4, Note: "11-40"},
		}),
		WithRows([]Row{{"", "1", "alice"}}),
		WithColNumbers(true),
		WithStyles(Styles{Header: lipgloss.NewStyle(), Cell: lipgloss.NewStyle(), Selected: lipgloss.NewStyle()}),
	)
	m.SetWidth(60)
	m.SetHeight(6)

	view := m.View()
	for _, want := range []string{"(1) 1-10", "(2) 11-40"} {
		if !strings.Contains(view, want) {
			t.Errorf("numbering line missing %q:\n%s", want, view)
		}
	}

	// The "id" column (base width 2) must have grown to hold "(1) 1-10".
	if w := m.Columns()[1].Width; w < len("(1) 1-10") {
		t.Errorf("note column width = %d, want >= %d", w, len("(1) 1-10"))
	}
}

// Without WithColNumbers the note line does not render at all.
func TestColumnNoteRequiresColNumbers(t *testing.T) {
	m := New(
		WithColumns([]Column{{Title: "id", Width: 2, Note: "1-10"}}),
		WithRows([]Row{{"1"}}),
		WithStyles(Styles{Header: lipgloss.NewStyle(), Cell: lipgloss.NewStyle(), Selected: lipgloss.NewStyle()}),
	)
	m.SetWidth(40)
	m.SetHeight(6)
	if strings.Contains(m.View(), "1-10") {
		t.Errorf("note must not render without column numbers:\n%s", m.View())
	}
}
