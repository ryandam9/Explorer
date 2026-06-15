package ui

import "github.com/charmbracelet/lipgloss"

// KeyAbout toggles the per-page "About" overlay, which explains what the
// current screen is for. Kept here so every TUI binds the same key.
const KeyAbout = "i"

// AboutView renders the shared "About this page" overlay: a bordered, themed
// modal with a bold title and a wrapped prose description of what the current
// screen does. Shared by every TUI (toggled with "i") so the About box looks
// and behaves identically everywhere.
//
// body is plain prose; it is wrapped to fit inside the box, so callers can pass
// long paragraphs without pre-formatting line breaks.
func AboutView(title, body string, width int) string {
	if width < 32 {
		width = 32
	}
	inner := width - 4 // the box pads 2 columns on each side
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText())).Width(inner)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render(title),
		"",
		bodyStyle.Render(body),
		"",
		hintStyle.Render("i / Esc  close"),
	)
	return lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Padding(1, 2).
		Render(content)
}

// AboutWidth picks a comfortable width for the About overlay given the terminal
// width: roomy but capped, and never wider than the screen. Shared so every
// page's About box is sized consistently.
func AboutWidth(termWidth int) int {
	w := termWidth - 12
	if w > 76 {
		w = 76
	}
	if w < 32 {
		w = 32
	}
	return w
}
