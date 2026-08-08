package cwtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The event record view ("v" on the events pane) shows the selected event
// vertically: every JSON field's full value — nothing clipped to a table
// cell — plus the timestamp, stream and any non-JSON remainder, hard-wrapped
// to the overlay width and scrollable.

// eventRecordEntry is one label/value pair of the record view.
type eventRecordEntry struct {
	label string
	value string
}

// eventRecordEntries flattens an event into ordered label/value pairs: Time,
// Stream (when present), each top-level JSON field in document order, and the
// raw remainder (the whole message for non-JSON events, or a Lambda-style
// prefix/suffix). Values are complete — clipping is the table's concern.
func eventRecordEntries(ev types.FilteredLogEvent) []eventRecordEntry {
	entries := []eventRecordEntry{{"Time", eventTimestamp(ev)}}
	if s := aws.ToString(ev.LogStreamName); s != "" {
		entries = append(entries, eventRecordEntry{"Stream", s})
	}
	fields, keys, raw := splitEventJSON(aws.ToString(ev.Message))
	for _, k := range keys {
		entries = append(entries, eventRecordEntry{k, fields[k]})
	}
	if raw != "" || fields == nil {
		entries = append(entries, eventRecordEntry{"Message", raw})
	}
	return entries
}

// eventRecordText renders entries as plain "label : value" text — the shared
// shape for the viewport content (wrapped) and the clipboard copy (as-is).
func eventRecordText(entries []eventRecordEntry) string {
	labelW := 0
	for _, e := range entries {
		if n := len([]rune(e.label)); n > labelW && n <= 32 {
			labelW = n
		}
	}
	var b strings.Builder
	for _, e := range entries {
		label := e.label
		if r := []rune(label); len(r) > 32 {
			label = string(r[:32])
		}
		fmt.Fprintf(&b, "%-*s : %s\n", labelW, label, e.value)
	}
	return strings.TrimRight(b.String(), "\n")
}

// eventRecordLines wraps the record text to the overlay width, indenting
// continuations under the value column so long values stay readable.
func eventRecordLines(entries []eventRecordEntry, width int) []string {
	if width < 20 {
		width = 20
	}
	labelW := 0
	for _, e := range entries {
		if n := len([]rune(e.label)); n > labelW && n <= 32 {
			labelW = n
		}
	}
	indent := strings.Repeat(" ", labelW+3)
	var lines []string
	for _, line := range strings.Split(eventRecordText(entries), "\n") {
		lines = append(lines, wrapLine(line, width, indent)...)
	}
	return lines
}

// recordOverlayWidth is the record overlay's inner text width.
func (m *model) recordOverlayWidth() int {
	w := m.width - 12
	if w > 110 {
		w = 110
	}
	if w < 30 {
		w = 30
	}
	return w
}

// openEventRecord opens the record view for the events panel's selection.
func (m *model) openEventRecord() {
	if len(m.events) == 0 || m.selectedEventIdx >= len(m.events) {
		return
	}
	m.openEventRecordFor(m.events[m.selectedEventIdx])
}

// openEventRecordFor opens the record view for any event — the events panel's
// selection or the log viewer table's highlighted row.
func (m *model) openEventRecordFor(ev types.FilteredLogEvent) {
	entries := eventRecordEntries(ev)
	w := m.recordOverlayWidth()
	h := m.height - 10
	if h < 5 {
		h = 5
	}
	m.recordText = eventRecordText(entries)
	m.recordVP = viewport.New(w, h)
	m.recordVP.SetContent(strings.Join(eventRecordLines(entries, w), "\n"))
	m.recordActive = true
}

// handleRecordKeys processes keys while the record view is open. It owns only
// its scrolling/copy/close keys; anything else is ignored so a stray key can't
// mutate the browser underneath.
func (m *model) handleRecordKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "v", "enter":
		m.recordActive = false
	case "up", "k":
		m.recordVP.ScrollUp(1)
	case "down", "j":
		m.recordVP.ScrollDown(1)
	case "pgup", "ctrl+u":
		m.recordVP.ScrollUp(m.recordVP.Height)
	case "pgdown", "ctrl+d":
		m.recordVP.ScrollDown(m.recordVP.Height)
	case "y":
		_ = clipboard.WriteAll(m.recordText)
		m.setToast("Copied event record to clipboard")
		*cmds = append(*cmds, toastCmd(3*time.Second))
	case ui.KeyHelp:
		m.showHelp = true
	}
}

// renderEventRecord renders the record overlay panel.
func (m *model) renderEventRecord() string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHeading())).
		Bold(true).
		Render("Event record")
	body := lipgloss.JoinVertical(lipgloss.Left, title, "", m.recordVP.View())
	panel := lipgloss.NewStyle().
		Width(m.recordOverlayWidth()+4).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Foreground(lipgloss.Color(ui.ColorText())).
		Padding(1, 2).
		Render(body)
	return panel
}
