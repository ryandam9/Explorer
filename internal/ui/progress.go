package ui

import (
	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
)

// ProgressBar renders a determinate progress bar, frac in [0, 1], width
// columns wide, filled with a two-colour gradient from the theme's heading
// colour to its accent (the superfile look) over a border-coloured track. It
// is stateless — built per render — so a theme switched in the settings panel
// recolours it on the next frame.
func ProgressBar(frac float64, width int) string {
	if width < 4 {
		width = 4
	}
	switch {
	case frac < 0:
		frac = 0
	case frac > 1:
		frac = 1
	}
	p := progress.New(
		progress.WithColors(lipgloss.Color(ColorHeading()), lipgloss.Color(ColorAccent())),
		progress.WithWidth(width),
		progress.WithoutPercentage(),
	)
	if c := ColorBorder(); c != "" {
		p.EmptyColor = lipgloss.Color(c)
	}
	return p.ViewAs(frac)
}

// Fraction is done/total as a progress fraction (0 when total is 0).
func Fraction(done, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(done) / float64(total)
}
