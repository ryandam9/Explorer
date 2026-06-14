package trailtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// chromeHeight is the height of everything below the header: the table panel's
// top and bottom borders, the column-scroll hint, and the status bar.
const chromeHeight = 2 /* panel border */ + 1 /* scroll hint */ + 1 /* status bar */

// layoutTable resizes the events table to the current terminal.
func (m *Model) layoutTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.tbl.SetWidth(m.width - 4) // panel border + padding
	h := m.height - lipgloss.Height(m.headerView()) - chromeHeight
	if h < 3 {
		h = 3
	}
	m.tbl.SetHeight(h)
}

func (m Model) View() string {
	if m.width <= 0 {
		return "Initializing…"
	}

	header := m.headerView()
	body := m.bodyView()
	status := m.statusBarView()

	view := lipgloss.JoinVertical(lipgloss.Left, header, body, status)
	view = ui.ClipToSize(view, m.width, m.height)

	switch m.overlay {
	case overlayDetail:
		view = ui.OverlayCenter(view, m.detailOverlay(), m.width, m.height)
	case overlayHelp:
		view = ui.OverlayCenter(view, m.helpOverlay(), m.width, m.height)
	}
	// The debug pane floats above any other overlay so it stays reachable.
	return m.debug.Overlay(view, m.width, m.height)
}

// headerView is two lines: the page name with the scope and load status, and a
// running tally (events, failed count, filter/toggle indicators).
func (m Model) headerView() string {
	title := ui.HeaderStyle().Render("CloudTrail activity")

	var status string
	switch {
	case m.loading:
		status = ui.MutedStyle().Render(m.spin.View() + " looking up events…")
	case m.loadErr != nil:
		status = ui.ErrorStyle().Render("✗ lookup failed")
	default:
		status = ui.MutedStyle().Render(m.scope)
	}
	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", status)
	if badge := ui.RegionBadge([]string{m.region}, false); badge != "" {
		line1 = lipgloss.JoinHorizontal(lipgloss.Top, line1, "  ", badge)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%d event(s)", len(m.all)))
	if failed := countFailed(m.all); failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if m.errorsOnly {
		parts = append(parts, "failed only")
	}
	if m.truncated {
		parts = append(parts, "scan cap reached — narrow with --since")
	}
	line2 := ui.MutedStyle().Render(strings.Join(parts, " · "))
	if q := m.filterIn.Value(); m.filtering || q != "" {
		line2 += "  " + m.filterIn.View() +
			ui.MutedStyle().Render(fmt.Sprintf(" %d/%d", len(m.visible), len(m.all)))
	}
	return line1 + "\n" + line2
}

func (m Model) bodyView() string {
	if m.loadErr != nil {
		msg := ui.ErrorStyle().Render("✗ "+m.loadErr.Error()) + "\n\n" +
			ui.MutedStyle().Render("CloudTrail LookupEvents needs the cloudtrail:LookupEvents permission.")
		return lipgloss.NewStyle().Padding(1, 2).Render(msg)
	}
	if !m.loading && len(m.all) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			ui.MutedStyle().Render("No management events recorded for " + m.scope + " in the lookup window."))
	}
	if !m.loading && len(m.visible) == 0 {
		hint := "No events match the current filter."
		if m.errorsOnly {
			hint = "No failed/denied calls in this feed. Press x to show all events."
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(ui.MutedStyle().Render(hint))
	}
	panel := ui.TablePanelStyle(true).Render(m.tbl.View())
	hint := ui.TableScrollIndicator(&m.tbl)
	return panel + "\n" + hint
}

func (m Model) statusBarView() string {
	return ui.StatusBar(m.width, m.status, m.keyHints())
}

// keyHints lists only the shortcuts usable right now.
func (m Model) keyHints() []ui.KeyHint {
	if m.filtering {
		return []ui.KeyHint{
			ui.H("Enter", "keep filter"),
			ui.H("Esc", "clear"),
		}
	}
	if m.overlay != overlayNone {
		return []ui.KeyHint{ui.H("Esc", "close")}
	}

	hints := []ui.KeyHint{
		ui.H("↑/↓", "navigate"),
		ui.H("Enter", "detail"),
		ui.H("/", "filter"),
		ui.H("x", "failed only"),
		ui.H("s", "sort"),
	}
	if m.sortCol > 0 {
		hints = append(hints, ui.H("R", "reverse"))
	}
	if hl, hr := m.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
	}
	hints = append(hints,
		ui.H("y", "copy"),
		ui.H("C", "csv"),
		ui.H("~", "debug"),
		ui.H("?", "help"),
		ui.H("q", "quit"),
	)
	return hints
}

// overlayStyle is the shared frame for the detail and help overlays.
func (m Model) overlayStyle() lipgloss.Style {
	w := m.width - 8
	if w > 84 {
		w = 84
	}
	if w < 30 {
		w = 30
	}
	return lipgloss.NewStyle().
		Width(w).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(1, 2)
}

func (m Model) detailOverlay() string {
	ev := m.selected()
	if ev == nil {
		return ""
	}
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(12)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 12)
	row := func(name, v string) string {
		if v == "" {
			v = "-"
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, label.Render(name), value.Render(v))
	}

	outcome := "ok"
	if ev.ErrorCode != "" {
		outcome = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render("✗ " + ev.ErrorCode)
	}

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(ev.EventName) + "\n\n")
	b.WriteString(row("Time", ev.Time.UTC().Format("2006-01-02 15:04:05 UTC")) + "\n")
	b.WriteString(row("Principal", ev.Principal) + "\n")
	b.WriteString(row("Source IP", ev.SourceIP) + "\n")
	b.WriteString(row("Read-only", fmt.Sprintf("%t", ev.ReadOnly)) + "\n")
	b.WriteString(row("Outcome", outcome) + "\n\n")
	b.WriteString(ui.MutedStyle().Render("y copies the event name · Esc closes"))
	return style.Render(b.String())
}

func (m Model) helpOverlay() string {
	style := m.overlayStyle()
	rows := []struct{ key, action string }{
		{"↑/↓, j/k", "Navigate events"},
		{"Enter", "Open the detail overlay for the selected event"},
		{"/", "Quick filter (matches any field, live matched/total count)"},
		{"x", "Toggle failed/denied calls only"},
		{"s / R", "Sort by the next column / reverse the direction"},
		{"r", "Reset filter, sort, and the failed-only toggle"},
		{"</> or ,/.", "Scroll columns when the table is wider than the screen"},
		{"y", "Copy the selected event's name"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"~", "Debug: live view of what the tool is doing"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("CloudTrail feed — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	return style.Render(b.String())
}
