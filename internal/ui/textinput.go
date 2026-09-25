package ui

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// StyleTextInput gives a text input the app's themed look: an accent prompt,
// body-colour text, a muted placeholder and an accent cursor — the same in
// the focused and blurred states (Bubbles v2 styles the two separately).
func StyleTextInput(ti *textinput.Model) { StyleTextInputPrompt(ti, ColorAccent()) }

// StyleTextInputPrompt is StyleTextInput with a custom prompt colour (e.g.
// the error colour for a destructive confirmation).
func StyleTextInputPrompt(ti *textinput.Model, promptColor string) {
	st := textinput.StyleState{
		Prompt:      lipgloss.NewStyle().Foreground(lipgloss.Color(promptColor)).Bold(true),
		Text:        lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText())),
		Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())),
	}
	s := ti.Styles()
	s.Focused, s.Blurred = st, st
	s.Cursor.Color = lipgloss.Color(ColorAccent())
	ti.SetStyles(s)
}
