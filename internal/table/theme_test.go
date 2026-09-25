package table

import (
	"testing"

	"charm.land/lipgloss/v2"
)

// With a theme source registered, a table re-reads its styles when the theme
// generation moves on — keeping its zebra choice — and not otherwise.
func TestThemeSourceRestyles(t *testing.T) {
	gen := uint64(1)
	calls := 0
	var gotZebra bool
	SetThemeSource(func() uint64 { return gen }, func(zebra bool) Styles {
		calls++
		gotZebra = zebra
		s := DefaultStyles()
		s.Selected = s.Selected.Foreground(lipgloss.Color("#00ff00"))
		if zebra {
			s.RowAlt = lipgloss.NewStyle().Background(lipgloss.Color("#222222"))
		}
		return s
	})
	defer SetThemeSource(nil, nil)

	zs := DefaultStyles()
	zs.RowAlt = lipgloss.NewStyle().Background(lipgloss.Color("#111111"))
	m := New(WithColumns([]Column{{Title: "A", Width: 3}}), WithRows([]Row{{"x"}}), WithStyles(zs))
	calls = 0
	_ = m.View()
	if calls != 0 {
		t.Fatalf("styles set at the current generation must not be re-read (%d calls)", calls)
	}
	gen++
	calls, gotZebra = 0, false
	_ = m.View()
	if calls == 0 || !gotZebra {
		t.Fatalf("a theme change must restyle, keeping zebra: calls=%d zebra=%v", calls, gotZebra)
	}
	m.UpdateViewport() // persists the new generation
	calls = 0
	_ = m.View()
	if calls != 0 {
		t.Errorf("after the table caught up, rendering must not re-read styles (%d calls)", calls)
	}
}
