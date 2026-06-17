package trailtui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/trail"
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
		view = ui.OverlayCenterBlank(m.detailOverlay(), m.width, m.height)
	case overlayHelp:
		view = ui.OverlayCenterBlank(m.helpOverlay(), m.width, m.height)
	case overlayAbout:
		view = ui.OverlayCenterBlank(ui.AboutView("About — CloudTrail Feed", trailAboutText, ui.AboutWidth(m.width)), m.width, m.height)
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
		prog := m.spin.View() + " looking up events…"
		if m.regionsTotal > 1 {
			prog = m.spin.View() + fmt.Sprintf(" scanning regions %d/%d…", m.regionsDone, m.regionsTotal)
		}
		status = ui.MutedStyle().Render(prog)
	case m.loadErr != nil:
		status = ui.ErrorStyle().Render("✗ lookup failed")
	default:
		status = ui.MutedStyle().Render(m.scope)
	}
	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", status)
	if badge := regionBadge(m.regions); badge != "" {
		line1 = lipgloss.JoinHorizontal(lipgloss.Top, line1, "  ", badge)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%d event(s)", len(m.visible)))
	if failed := countFailed(m.visible); failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if m.errorsOnly {
		parts = append(parts, "failed only")
	}
	if m.capped {
		parts = append(parts, fmt.Sprintf("showing newest %d — raise with --limit", m.limit))
	} else if m.truncated {
		parts = append(parts, "scan cap reached — narrow with --since")
	}
	if !m.loading && len(m.regionErrs) > 0 {
		parts = append(parts, fmt.Sprintf("%d region(s) failed", len(m.regionErrs)))
	}
	line2 := ui.MutedStyle().Render(strings.Join(parts, " · "))
	if q := m.filterIn.Value(); m.filtering || q != "" {
		line2 += "  " + m.filterIn.View() +
			ui.MutedStyle().Render(fmt.Sprintf(" %d/%d", len(m.visible), len(m.all)))
	}
	return line1 + "\n" + line2
}

// regionBadge spotlights a single scanned region, or names the count when the
// feed spans several (RegionBadge would list them all, which is too long).
func regionBadge(regions []string) string {
	if len(regions) == 1 {
		return ui.RegionBadge(regions, false)
	}
	if len(regions) > 1 {
		return ui.RegionBadgeStyle().Render("◉ " + regionLabel(regions))
	}
	return ""
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
		ui.H("i", "about"),
		ui.H("?", "help"),
		ui.H("q", "quit"),
	)
	return hints
}

// trailAboutText explains what the CloudTrail feed TUI is for, shown in the
// About overlay ("i").
const trailAboutText = "This is the CloudTrail activity feed. It answers \"who changed this, and " +
	"when?\" and \"what has been happening in this account?\" — each row is an API " +
	"call with its time, principal, source IP and whether it failed, newest first.\n\n" +
	"The scope (a resource, a principal, an API, a service, or the whole account) " +
	"and region are set by the command's flags; this screen makes that feed " +
	"navigable. Press Enter for a per-event detail overlay, / to filter, x to show " +
	"only failed/denied calls, and C to export.\n\n" +
	"It uses the zero-setup 90-day LookupEvents window. Press ? for the full list " +
	"of keyboard shortcuts."

// overlayStyle is the shared frame for the detail and help overlays.
func (m Model) overlayStyle() lipgloss.Style {
	w := m.width - 8
	if w > 96 {
		w = 96
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

// detailField is one label/value pair shown in the detail overlay.
type detailField struct {
	name string
	val  string
	// isError marks the outcome of a failed call so the rendered overlay can
	// colour it; the plain-text copy ignores the flag.
	isError bool
}

// detailFields is the single source of truth for the per-event detail panel,
// shared by the rendered overlay and the plain-text clipboard copy. Optional
// fields CloudTrail didn't record are dropped so both forms stay compact;
// required fields fall back to "-".
func detailFields(ev *trail.Event) []detailField {
	agent := ev.UserAgent
	if ev.FromConsole {
		agent = strings.TrimSpace(agent + " (console)")
	}
	outcome := "ok"
	if ev.ErrorCode != "" {
		outcome = "✗ " + ev.ErrorCode
	}

	// opt marks fields hidden when empty; the rest fall back to "-".
	type spec struct {
		name, val string
		opt       bool
	}
	specs := []spec{
		{"Time", ev.Time.UTC().Format("2006-01-02 15:04:05 UTC"), false},
		{"Service", ev.EventSource, true},
		{"Region", ev.Region, true},
		{"Principal", ev.Principal, false},
		{"Access key", ev.AccessKeyID, true},
		{"Source IP", ev.SourceIP, false},
		{"User agent", agent, true},
		{"MFA", fmt.Sprintf("%t", ev.MFA), false},
		{"Read-only", fmt.Sprintf("%t", ev.ReadOnly), false},
		{"Outcome", outcome, false},
		{"Error", ev.ErrorMessage, true},
		{"Resources", resourcesText(ev.Resources), true},
		{"Event ID", ev.EventID, true},
	}

	var fields []detailField
	for _, s := range specs {
		v := s.val
		if v == "" {
			if s.opt {
				continue
			}
			v = "-"
		}
		fields = append(fields, detailField{
			name:    s.name,
			val:     v,
			isError: s.name == "Outcome" && ev.ErrorCode != "",
		})
	}
	return fields
}

func (m Model) detailOverlay() string {
	ev := m.selected()
	if ev == nil {
		return ""
	}
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(14)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 14)
	errValue := value.Foreground(lipgloss.Color(ui.ColorError()))

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(ev.EventName) + "\n\n")
	for _, f := range detailFields(ev) {
		val := value
		if f.isError {
			val = errValue
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Render(f.name), val.Render(f.val)) + "\n")
	}
	b.WriteString("\n" + ui.MutedStyle().Render("y copies these details · Esc closes"))
	return style.Render(b.String())
}

// detailText renders the event detail as plain, unstyled text for the
// clipboard, so the copied panel can be pasted into a message or ticket
// without ANSI escapes or the surrounding table.
func detailText(ev *trail.Event) string {
	var b strings.Builder
	b.WriteString(ev.EventName + "\n\n")
	for _, f := range detailFields(ev) {
		b.WriteString(fmt.Sprintf("%-11s %s\n", f.name+":", f.val))
	}
	return strings.TrimRight(b.String(), "\n")
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
		{"y", "Copy the event name (or the whole detail panel when it's open)"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"~", "Debug: live view of what the tool is doing"},
		{"i", "About this page (what it does)"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	sort.SliceStable(rows, func(i, j int) bool { return ui.SortKeyLess(rows[i].key, rows[j].key) })
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("CloudTrail feed — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	return style.Render(b.String())
}
