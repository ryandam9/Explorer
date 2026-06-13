package ui

import "github.com/charmbracelet/lipgloss"

// Global key bindings shared by every TUI in the application. Keeping these in
// one place means a key means the same thing everywhere (e.g. "S" always opens
// Settings, "?" always toggles help), and new per-service browsers can reuse
// them instead of inventing their own conventions.
const (
	// KeySettings opens the shared theme/colors settings panel.
	KeySettings = "S"
	// KeyHelp toggles the help overlay.
	KeyHelp = "?"
	// KeyQuit exits the application.
	KeyQuit = "q"
	// KeyDebug toggles the debug activity overlay, which shows the live stream
	// of what the tool is doing (regions, services, API calls and errors).
	KeyDebug = "~"
)

// HelpView renders a bordered, themed help overlay from a title and a block of
// pre-formatted body lines. Shared by all TUIs so every help screen looks and
// behaves the same.
func HelpView(title, body string, width int) string {
	inner := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading())).Render(title),
		"",
		body,
	)
	return lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(1, 2).
		Render(inner)
}
