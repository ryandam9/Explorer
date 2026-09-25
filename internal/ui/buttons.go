package ui

import "charm.land/lipgloss/v2"

// ConfirmButtons renders the pair of buttons at the foot of a confirmation
// modal — a filled confirm button and a quieter cancel one, the way
// superfile's modals read — e.g. ConfirmButtons("y  Download", "Esc  Cancel",
// false). Labels name the key that acts, since the buttons are not focusable:
// the keys work exactly as before, the buttons just make the choice
// unmissable. danger colours the confirm button as an error, for actions that
// mutate or cost (the typed S3 delete gate).
func ConfirmButtons(confirm, cancel string, danger bool) string {
	ink := ColorCanvas()
	if ink == "" {
		ink = "#1a1a1a" // dark text on the bright button when no canvas is set
	}
	fill := ColorSuccess()
	if danger {
		fill = ColorError()
	}
	ok := lipgloss.NewStyle().Bold(true).Padding(0, 2).
		Foreground(lipgloss.Color(ink)).Background(lipgloss.Color(fill)).Render(confirm)
	no := lipgloss.NewStyle().Padding(0, 2).
		Foreground(lipgloss.Color(ColorText())).Background(lipgloss.Color(ColorBorder())).Render(cancel)
	return ok + "   " + no
}
