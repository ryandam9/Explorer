package ui

import (
	"sort"
	"testing"
)

func TestSortKeyLessOrders(t *testing.T) {
	keys := []string{"q", "Enter", "?", "s / R", "↑/↓", "C", "Esc", "/", "a", "S"}
	sort.SliceStable(keys, func(i, j int) bool { return SortKeyLess(keys[i], keys[j]) })

	// Symbols/navigation first (rune order), then letters alphabetically.
	want := []string{"/", "?", "↑/↓", "a", "C", "Enter", "Esc", "q", "S", "s / R"}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("position %d: got %q, want %q\nfull: %v", i, keys[i], want[i], keys)
		}
	}
}

func TestSortKeyLessLowercaseBeforeUppercaseTwin(t *testing.T) {
	if !SortKeyLess("l", "L") {
		t.Error(`"l" should sort before its uppercase twin "L"`)
	}
	if SortKeyLess("L", "l") {
		t.Error(`"L" should not sort before "l"`)
	}
}

func TestSortHelpSectionsPreservesGrouping(t *testing.T) {
	in := []string{
		"Navigation",
		"  Tab        Switch focus",
		"  Enter      Select",
		"  ↑/↓        Move",
		"",
		"Utility",
		"  ?          Help",
		"  S          Settings",
		"  i          About",
	}
	got := SortHelpSections(in)

	want := []string{
		"Navigation",
		"  ↑/↓        Move",
		"  Enter      Select",
		"  Tab        Switch focus",
		"",
		"Utility",
		"  ?          Help",
		"  i          About",
		"  S          Settings",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d: got %q, want %q\nfull: %#v", i, got[i], want[i], got)
		}
	}
}

func TestHelpEntryKey(t *testing.T) {
	cases := map[string]string{
		"  L                  Open the logs explorer": "L",
		"  ↑/↓, [ ]           Move selection":         "↑/↓, [ ]",
		"  s / R              Sort":                   "s / R",
		"  q, Ctrl+C          Quit":                   "q, Ctrl+C",
	}
	for line, want := range cases {
		if got := helpEntryKey(line); got != want {
			t.Errorf("helpEntryKey(%q) = %q, want %q", line, got, want)
		}
	}
}
