package table

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestZebraDetection(t *testing.T) {
	plain := New(WithStyles(Styles{Cell: lipgloss.NewStyle()}))
	if plain.zebra {
		t.Error("no RowAlt background → zebra must be off")
	}
	z := New(WithStyles(Styles{
		Cell:   lipgloss.NewStyle(),
		RowAlt: lipgloss.NewStyle().Background(lipgloss.Color("236")),
	}))
	if !z.zebra {
		t.Fatal("RowAlt background set → zebra must be on")
	}
}

func TestZebraStripesOddRows(t *testing.T) {
	// Force a colour profile so the background escape is emitted in the non-TTY
	// test environment (lipgloss otherwise downgrades to plain ASCII).
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := New(
		WithColumns([]Column{{Title: "C", Width: 6}}),
		WithRows([]Row{{"r0"}, {"r1"}, {"r2"}}),
		WithStyles(Styles{
			Cell:     lipgloss.NewStyle(),
			Selected: lipgloss.NewStyle(),
			RowAlt:   lipgloss.NewStyle().Background(lipgloss.Color("236")),
		}),
	)
	m.SetWidth(10)
	m.SetHeight(6)
	m.SetCursor(2) // keep the cursor off rows 0 and 1 so striping is observable

	vis := m.visibleCols()
	row0 := m.renderRow(0, vis)
	row1 := m.renderRow(1, vis)
	if strings.Contains(row0, "48;5;236") {
		t.Errorf("even row should not be striped: %q", row0)
	}
	if !strings.Contains(row1, "48;5;236") {
		t.Errorf("odd row should be striped: %q", row1)
	}
}
