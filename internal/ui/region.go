package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RegionBadgeStyle is the high-contrast badge used to spotlight the active
// region scope at the top of a screen — a filled highlight background so the
// region a user is looking at can never be mistaken.
func RegionBadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorHighlightText())).
		Background(lipgloss.Color(ColorHighlight())).
		Bold(true).
		Padding(0, 1)
}

// RegionBadge returns a distinctively styled badge naming the active region
// scope, so a user scanning a fixed region set is never confused about which
// region a resource belongs to.
//
// It returns "" when allRegions is true (the scope spans everything, so there
// is no single region worth spotlighting) or when no concrete regions are
// known. Callers append it to a header only when it is non-empty, which keeps
// the all-regions view unchanged.
func RegionBadge(regions []string, allRegions bool) string {
	if allRegions {
		return ""
	}
	var uniq []string
	seen := make(map[string]bool)
	for _, r := range regions {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		uniq = append(uniq, r)
	}
	if len(uniq) == 0 {
		return ""
	}
	label := "◉ Region: " + uniq[0]
	if len(uniq) > 1 {
		label = "◉ Regions: " + strings.Join(uniq, ", ")
	}
	return RegionBadgeStyle().Render(label)
}
