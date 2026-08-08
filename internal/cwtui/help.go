package cwtui

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

// helpSections is the full key reference for the CloudWatch Logs explorer,
// grouped by the pane the keys act on. The status bar shows only the keys
// usable right now (eliding on narrow terminals), so this page is the one
// place every binding is guaranteed to be visible.
func helpSections() []helpSection {
	type row = struct{ key, action string }
	return []helpSection{
		{"Log groups (sidebar)", []row{
			{"↑/↓, j/k", "Navigate groups (streams load eagerly)"},
			{"Enter", "Open the group's streams"},
			{"/", "Filter groups by name or region"},
			{"G", "Search events across the whole group (all streams)"},
			{"D", "Download the group's events in the query window to a file"},
			{"o", "Copy the console URL (opens the browser when local)"},
		}},
		{"Log streams", []row{
			{"↑/↓, j/k", "Navigate streams"},
			{"Enter", "List the stream's events"},
			{"/", "Filter streams by name"},
			{"D", "Download the selected stream's events to a file"},
			{"Esc, Backspace", "Back to the group list"},
		}},
		{"Events", []row{
			{"↑/↓, j/k", "Navigate events"},
			{"Enter", "Open the full log viewer"},
			{"v", "Record view: every field of the event in full, unclipped"},
			{"/", "Server-side query pattern (CloudWatch filter syntax)"},
			{"p", "Cycle the query window: 30m → 1h → … → 24h → 3d → 7d"},
			{"t", "Toggle the zebra-striped table view"},
			{"J", "Table view: split JSON events into one column per field"},
			{"←/→", "Table view: pan long messages (hidden columns first)"},
			{"W", "Toggle live tail watch mode"},
			{"y", "Copy the selected event"},
			{"s", "Export the listed events to the downloads directory"},
			{"D", "Download every event in the window (full fetch, not just the listed page)"},
			{"Esc, Backspace", "Back to streams (or groups from a whole-group search)"},
		}},
		{"Event record (v)", []row{
			{"↑/↓, j/k", "Scroll the record"},
			{"PgUp, PgDn", "Page up / down (also Ctrl+U / Ctrl+D)"},
			{"y", "Copy the full record to the clipboard"},
			{"Esc, q, Enter", "Close (v closes too)"},
		}},
		{"Full log viewer", []row{
			{"↑/↓, j/k", "Scroll (scrolling up pauses tailing)"},
			{"PgUp, PgDn", "Page up / down (also Ctrl+U / Ctrl+D)"},
			{"g/Home, G/End", "Jump to top / bottom (bottom resumes tailing)"},
			{"f", "Toggle follow (auto-scroll on new events)"},
			{"J", "Pretty-print JSON embedded in messages"},
			{"/", "Find in the log"},
			{"n / N", "Jump to the next / previous find match"},
			{"&", "Grep filter: render only lines matching a regex"},
			{"y / s", "Copy / export the log (matches only while grepping)"},
			{"Esc, q", "Close the viewer"},
		}},
		{"Everywhere", []row{
			{"Tab, Shift+Tab", "Cycle panel focus forward / back"},
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
	return ui.HelpView("CloudWatch Logs — keys", strings.TrimRight(b.String(), "\n"), w)
}
