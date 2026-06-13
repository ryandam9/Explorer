package billtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/billing"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// chromeHeight is the height of everything below the header: the table panel's
// top and bottom borders, the column-scroll hint, and the status bar. The
// header's height is measured separately (it varies — title, PAID badge/state,
// totals — and ui.HeaderStyle adds a bottom margin), because under-counting it
// makes the frame too tall and ClipToSize trims the status bar off the bottom.
const chromeHeight = 2 /* panel border */ + 1 /* scroll hint */ + 1 /* status bar */

// layoutTable resizes the bill table to the current terminal.
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
		return ui.OverlayCenter(view, m.detailOverlay(), m.width, m.height)
	case overlayResources:
		return ui.OverlayCenter(view, m.resourcesOverlay(), m.width, m.height)
	case overlayHelp:
		return ui.OverlayCenter(view, m.helpOverlay(), m.width, m.height)
	}
	return view
}

// headerView is two lines: the page name with a PAID badge and refresh
// state, and the running total with line count and refresh cadence.
func (m Model) headerView() string {
	title := ui.HeaderStyle().Render("Bill — " + m.label)
	paid := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorStatusBarText())).
		Background(lipgloss.Color(ui.ColorWarning())).
		Bold(true).Padding(0, 1).Render("PAID")

	var state string
	switch {
	case m.fetching:
		state = ui.MutedStyle().Render(m.spin.View() + " refreshing…")
	case !m.updated.IsZero():
		state = ui.MutedStyle().Render("updated " + m.updated.Format("15:04:05"))
	}
	// JoinHorizontal (not string concat) so the badge and state sit on the
	// title's first row; ui.HeaderStyle's bottom margin would otherwise push
	// them onto the blank second row.
	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", paid, "  ", state)

	var parts []string
	if m.bill != nil {
		total := "total " + billing.FormatAmount(m.bill.Total, m.bill.Currency)
		if m.bill.Estimated {
			total += " (estimated)"
		}
		parts = append(parts, total, fmt.Sprintf("%d line item(s)", len(m.bill.Lines)))
	}
	parts = append(parts, fmt.Sprintf("auto-refresh %s", m.interval))
	line2 := ui.MutedStyle().Render(strings.Join(parts, " · "))
	line2 += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).
		Render("· Cost Explorer is a paid API — every refresh is a $0.01 request")
	if m.fetchErr != "" {
		line2 += "  " + ui.ErrorStyle().Render("⚠ "+m.fetchErr)
	}
	if q := m.filter.Value(); m.filtering || q != "" {
		var total int
		if m.bill != nil {
			total = len(m.bill.Lines)
		}
		line2 += "  " + m.filter.View() +
			ui.MutedStyle().Render(fmt.Sprintf(" %d/%d", len(m.visible), total))
	}
	return line1 + "\n" + line2
}

func (m Model) bodyView() string {
	if m.bill != nil && len(m.bill.Lines) == 0 && !m.fetching {
		msg := ui.SuccessStyle().Render("✓ Nothing billed in this period.")
		return lipgloss.NewStyle().Padding(1, 2).Render(msg)
	}
	panel := ui.TablePanelStyle(true).Render(m.tbl.View())
	hint := ui.TableScrollIndicator(&m.tbl)
	return panel + "\n" + hint
}

func (m Model) statusBarView() string {
	left := m.status
	if left == "" {
		left = m.sortLabel()
	}
	return ui.StatusBar(m.width, left, m.keyHints())
}

// sortLabel names the active sort for the status bar.
func (m Model) sortLabel() string {
	if m.sortCol < 0 {
		return "sorted by cost"
	}
	dir := "↓"
	if m.sortAsc {
		dir = "↑"
	}
	return "sorted by " + strings.ToLower(columns[m.sortCol].Title) + " " + dir
}

// keyHints lists only the shortcuts usable right now, per the app-wide
// context-aware status bar convention.
func (m Model) keyHints() []ui.KeyHint {
	if m.filtering {
		return []ui.KeyHint{
			ui.H("Enter", "keep filter"),
			ui.H("Esc", "clear"),
		}
	}
	if m.overlay == overlayResources {
		return []ui.KeyHint{
			ui.H("↑/↓", "scroll"),
			ui.H("Esc", "close"),
		}
	}
	if m.overlay != overlayNone {
		return []ui.KeyHint{ui.H("Esc", "close")}
	}

	hints := []ui.KeyHint{
		ui.H("↑/↓", "navigate"),
		ui.H("Enter", "detail"),
		ui.H("x", "resources"),
		ui.H("u", "refresh"),
		ui.H("/", "filter"),
		ui.H("s", "sort"),
	}
	if m.sortCol >= 0 {
		hints = append(hints, ui.H("R", "reverse"))
	}
	if hl, hr := m.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
	}
	hints = append(hints,
		ui.H("y", "copy"),
		ui.H("C", "csv"),
		ui.H("?", "help"),
		ui.H("q", "quit"),
	)
	return hints
}

// overlayStyle is the shared frame for all overlays.
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

func (m Model) detailOverlay() string {
	l := m.selected()
	if l == nil {
		return ""
	}
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
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

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(l.Service) + "\n\n")
	b.WriteString(row("Usage type", l.UsageType) + "\n")
	b.WriteString(row("Usage", billing.FormatQty(l.Quantity)+" "+l.Unit) + "\n")
	b.WriteString(row("Cost", fmt.Sprintf("%s (%.6f %s)", billing.FormatAmount(l.Amount, currency), l.Amount, currency)) + "\n")
	if d := m.deltas[l.Key()]; d != 0 {
		b.WriteString(row("Δ refresh", formatDelta(d, currency)) + "\n")
	}
	b.WriteString(row("Period", m.start.Format("2006-01-02")+" → "+m.end.Format("2006-01-02")) + "\n\n")
	b.WriteString(ui.MutedStyle().Render("x lists this service's resources · y copies the line · Esc closes"))
	return style.Render(b.String())
}

func (m Model) resourcesOverlay() string {
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("Resources — "+m.resService) + "\n")
	b.WriteString(ui.MutedStyle().Width(w).Render(
		fmt.Sprintf("Cost Explorer keeps resource-level data for %d days (since %s), so these totals cover that window, not the whole bill.",
			14, m.resStart.Format("2006-01-02"))) + "\n\n")

	switch {
	case m.resFetching:
		b.WriteString(m.spin.View() + " fetching resource costs…")
	case m.resErr != "":
		b.WriteString(ui.ErrorStyle().Width(w).Render("⚠ " + m.resErr))
	case len(m.resRows) == 0:
		b.WriteString(ui.MutedStyle().Render("No resource-level cost recorded for this service in the window."))
	default:
		// Window the list around the scroll position so long lists fit.
		maxLines := m.height - 14
		if maxLines < 4 {
			maxLines = 4
		}
		start := m.resScroll
		if start > len(m.resRows)-1 {
			start = len(m.resRows) - 1
		}
		end := start + maxLines
		if end > len(m.resRows) {
			end = len(m.resRows)
		}
		amount := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(12)
		for _, r := range m.resRows[start:end] {
			line := lipgloss.JoinHorizontal(lipgloss.Top,
				amount.Render(billing.FormatAmount(r.Amount, currency)),
				lipgloss.NewStyle().Width(w-12).Render(
					fmt.Sprintf("%s  (%s %s)", r.Resource, billing.FormatQty(r.Quantity), r.Unit)))
			b.WriteString(line + "\n")
		}
		if end < len(m.resRows) {
			b.WriteString(ui.MutedStyle().Render(fmt.Sprintf("… %d more (↓ to scroll)", len(m.resRows)-end)))
		}
	}
	return style.Render(b.String())
}

func (m Model) helpOverlay() string {
	style := m.overlayStyle()
	rows := []struct{ key, action string }{
		{"↑/↓, j/k", "Navigate bill lines"},
		{"Enter", "Open the detail overlay for the selected line"},
		{"x", "Per-resource breakdown for the selected service (needs resource-level data enabled)"},
		{"u", "Refresh now (PAID — one $0.01 Cost Explorer request)"},
		{"/", "Quick filter (matches service, usage type, unit)"},
		{"s / R", "Sort by the next column / reverse the direction"},
		{"r", "Reset filter and sort"},
		{"</> or ,/.", "Scroll columns when the table is wider than the screen"},
		{"y", "Copy the selected service and usage type"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("Bill — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Bold(true).
		Render("PAID FEATURE: Cost Explorer bills $0.01 per request. ") +
		ui.MutedStyle().Render("Every automatic refresh at the configured --interval is one such request; the Δ column shows what moved since the previous refresh."))
	return style.Render(b.String())
}
