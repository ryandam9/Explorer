package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func testSections() []HelpSection {
	return []HelpSection{
		{"Alpha", []HelpRow{
			{"a", "First action with a reasonably long description that wraps"},
			{"b", "Second action"},
			{"c", "Third action"},
		}},
		{"Beta", []HelpRow{
			{"d", "Fourth action"},
			{"e", "Fifth action"},
			{"f", "Sixth action"},
			{"g", "Seventh action"},
		}},
		{"Gamma", []HelpRow{
			{"h", "Eighth action"},
			{"i", "Ninth action"},
		}},
	}
}

func TestHelpColumnsViewShorterThanSingleColumn(t *testing.T) {
	wide := HelpColumnsView("keys", testSections(), 140)
	narrow := HelpColumnsView("keys", testSections(), 60)

	// The two-column layout must actually reduce height — that's its point.
	if lipgloss.Height(wide) >= lipgloss.Height(narrow) {
		t.Errorf("two-column overlay (h=%d) should be shorter than one-column (h=%d)",
			lipgloss.Height(wide), lipgloss.Height(narrow))
	}

	// Every key and section title survives in both layouts.
	for _, out := range []string{wide, narrow} {
		for _, want := range []string{"Alpha", "Beta", "Gamma", "First action", "Seventh action", "Ninth action"} {
			if !strings.Contains(out, want) {
				t.Errorf("layout missing %q", want)
			}
		}
	}
}

func TestBalanceSplitKeepsOrderAndBalances(t *testing.T) {
	// One tall section then small ones: the split should put the tall one
	// alone on the left rather than stacking everything on one side.
	split := balanceSplit([]int{10, 3, 3, 3})
	if split != 1 {
		t.Errorf("balanceSplit = %d, want 1 (tall section alone on the left)", split)
	}
	// Uniform sections split near the middle.
	split = balanceSplit([]int{4, 4, 4, 4})
	if split != 2 {
		t.Errorf("balanceSplit = %d, want 2", split)
	}
}

func TestSoftWrapCells(t *testing.T) {
	lines := softWrapCells("short", 20)
	if len(lines) != 1 || lines[0] != "short" {
		t.Errorf("short text should not wrap, got %v", lines)
	}

	lines = softWrapCells("one two three four five six seven", 12)
	for _, l := range lines {
		if len(l) > 12 {
			t.Errorf("wrapped line %q exceeds width", l)
		}
	}
	if got := strings.Join(lines, " "); got != "one two three four five six seven" {
		t.Errorf("wrapping must not lose words, got %q", got)
	}

	// A single over-wide token hard-breaks instead of overflowing.
	lines = softWrapCells("aaaaaaaaaaaaaaaaaaaaaaaa", 10)
	for _, l := range lines {
		if len(l) > 10 {
			t.Errorf("hard-broken line %q exceeds width", l)
		}
	}
}
