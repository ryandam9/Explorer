package sqstui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// helpSection is one titled block of key/action rows in the help overlay.
type helpSection struct {
	title string
	rows  []struct{ key, action string }
}

// helpSections is the full key reference for the SQS explorer, grouped by the
// view the keys act on. The status bar shows only the keys usable right now
// (eliding on narrow terminals), so this page is the one place every binding
// is guaranteed to be visible.
func helpSections() []helpSection {
	type row = struct{ key, action string }
	return []helpSection{
		{"Queues (sidebar + overview)", []row{
			{"↑/↓, j/k", "Navigate queues (details load eagerly, cached per queue)"},
			{"/", "Filter queues by name or region"},
			{"P", "Peek at a sample of the queue's messages (confirmation states the receive-count side effect)"},
			{"d", "Jump to the queue's dead-letter queue (redrive target)"},
			{"m", "Toggle CloudWatch metric sparklines (refresh floored to 1m — paid API)"},
			{"L", "Open the CloudWatch Logs explorer for the queue's Lambda consumer"},
			{"o", "Copy the console URL (opens the browser when local)"},
			{"y", "Copy the queue URL"},
			{"r", "Refresh the selected queue's attributes (and metrics if shown)"},
			{"R", "Reload the queue list"},
		}},
		{"Messages (after a peek)", []row{
			{"↑/↓, j/k", "Navigate the sampled messages"},
			{"PgUp, PgDn", "Page up / down (also Ctrl+U / Ctrl+D)"},
			{"g/Home, G/End", "Jump to first / last message"},
			{"v, Enter", "Record view: the message's full body and every attribute, unclipped"},
			{"y", "Copy the selected message's body"},
			{"s", "Export the sampled messages to the downloads directory"},
			{"P", "Re-peek (another sample — confirmation shown again)"},
			{"Esc, Backspace", "Back to the queue overview"},
		}},
		{"Message record (v)", []row{
			{"↑/↓, j/k", "Scroll the record"},
			{"PgUp, PgDn", "Page up / down (also Ctrl+U / Ctrl+D)"},
			{"y", "Copy the full record to the clipboard"},
			{"Esc, q, Enter", "Close (v closes too)"},
		}},
		{"Everywhere", []row{
			{"~", "Debug: live view of what the tool is doing"},
			{"i", "About this page"},
			{"?", "Toggle this help"},
			{"q, Ctrl+C", "Quit"},
		}},
	}
}

// helpOverlay renders the shared help page ("?") for this TUI.
func (m *model) helpOverlay() string {
	w := m.width - 8
	if w > 96 {
		w = 96
	}
	if w < 30 {
		w = 30
	}

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(16)
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Bold(true)

	var b strings.Builder
	for i, sec := range helpSections() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sectionStyle.Render(sec.title) + "\n")
		for _, r := range sec.rows {
			b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
		}
	}
	return ui.HelpView("SQS Queues — keys", strings.TrimRight(b.String(), "\n"), w)
}
