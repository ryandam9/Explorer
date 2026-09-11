package cwtui

import (
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
		{"[1] Log groups (sidebar)", []row{
			{"↑/↓, j/k", "Navigate groups (streams load eagerly)"},
			{"Enter", "Open the group's streams"},
			{"/", "Filter groups by name or region"},
			{"G", "Search events across the whole group (all streams) — prompts for pattern(s) when none is set; Enter on the empty prompt browses everything"},
			{"D", "Download the group's events in the query window to a file"},
			{"o", "Copy the console URL (opens the browser when local)"},
		}},
		{"[2] Log streams", []row{
			{"↑/↓, j/k", "Navigate streams"},
			{"Enter", "List the stream's events"},
			{"/", "Filter streams by name (client-side, cosmetic)"},
			{"F", "Find which streams contain a string: one group-wide query, tallied per stream (matches, first/last), over the current window. Enter on a result opens that stream with the pattern applied"},
			{"D", "Download the selected stream's events to a file"},
			{"Esc, Backspace", "Back to the group list (clears a stream tally first)"},
		}},
		{"[3] Log events", []row{
			{"↑/↓, j/k", "Navigate events"},
			{"Enter", "Open the full log viewer"},
			{"v", "Record view: every field of the event in full, unclipped"},
			{"/", "Server-side query pattern(s) — CloudWatch filter syntax; several patterns separated by ; are OR'd, each running as its own query"},
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
		{"Log viewer (full-screen, Enter on an event)", []row{
			{"↑/↓, j/k", "Scroll (scrolling up pauses tailing)"},
			{"PgUp, PgDn", "Page up / down (also Ctrl+U / Ctrl+D)"},
			{"g/Home, G/End", "Jump to top / bottom (bottom resumes tailing)"},
			{"f", "Toggle follow (auto-scroll on new events)"},
			{"t", "Table view of the streamed events — same table as the events panel, with JSON splitting (J), panning (←/→), record view (v) and per-row copy (y)"},
			{"J", "Pretty-print JSON embedded in messages"},
			{"/", "Find in the log"},
			{"n / N", "Jump to the next / previous find match"},
			{"&", "Grep filter: render only lines matching a regex"},
			{"y / s", "Copy / export the log (matches only while grepping)"},
			{"Esc, q", "Close the viewer"},
		}},
		{"Everywhere", []row{
			{"C", "Clear all filters (list filters + event pattern; find & grep in the log viewer)"},
			{"Tab, Shift+Tab", "Cycle panel focus forward / back"},
			{"~", "Debug: live view of what the tool is doing"},
			{"i", "About this page"},
			{"?", "Toggle this help"},
			{"q, Ctrl+C", "Quit"},
		}},
	}
}

// helpOverlay renders the shared help page ("?") for this TUI. The key
// reference outgrew one column, so it flows into two on wide terminals
// (ui.HelpColumnsView falls back to one column when the terminal is narrow).
func (m *model) helpOverlay() string {
	secs := make([]ui.HelpSection, 0, 8)
	for _, s := range helpSections() {
		rows := make([]ui.HelpRow, 0, len(s.rows))
		for _, r := range s.rows {
			rows = append(rows, ui.HelpRow{Key: r.key, Action: r.action})
		}
		secs = append(secs, ui.HelpSection{Title: s.title, Rows: rows})
	}
	return ui.HelpColumnsView("CloudWatch Logs — keys", secs, m.width)
}
