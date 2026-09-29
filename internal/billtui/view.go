package billtui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ryandam9/aws_explorer/internal/billing"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// chromeHeight is the height of everything below the header: the table panel's
// top and bottom borders and the status bar. The
// header's height is measured separately (it varies — title, PAID badge/state,
// totals — and ui.HeaderStyle adds a bottom margin), because under-counting it
// makes the frame too tall and ClipToSize trims the status bar off the bottom.
const chromeHeight = 2 /* panel border (carries the row position and hidden-column marker) */ + 1 /* status bar */

// layoutTable resizes the bill table to the current terminal.
func (m *Model) layoutTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.tbl.SetWidth(m.width - 4) // panel border + padding
	h := m.height - lipgloss.Height(m.headerView()) - chromeHeight
	// The footer is conditional (summary total, hidden zero-cost lines), so
	// its height is measured rather than folded into chromeHeight — assuming
	// it away is what clips the status bar off the bottom.
	if f := m.footerView(); f != "" {
		h -= lipgloss.Height(f)
	}
	if h < 3 {
		h = 3
	}
	m.tbl.SetHeight(h)
}

func (m Model) viewString() string {
	if m.width <= 0 {
		return "Initializing…"
	}

	parts := []string{m.headerView(), m.bodyView()}
	if f := m.footerView(); f != "" {
		parts = append(parts, f)
	}
	parts = append(parts, m.statusBarView())

	view := lipgloss.JoinVertical(lipgloss.Left, parts...)
	view = ui.ClipToSize(view, m.width, m.height)

	switch m.overlay {
	case overlayDetail:
		view = ui.OverlayCenterBlank(m.detailOverlay(), m.width, m.height)
	case overlayResources:
		view = ui.OverlayCenterBlank(m.resourcesOverlay(), m.width, m.height)
	case overlayHelp:
		view = ui.OverlayCenterBlank(m.helpOverlay(), m.width, m.height)
	case overlayAbout:
		view = ui.OverlayCenterBlank(ui.AboutView("About — Live Bill", billAboutText, ui.AboutWidth(m.width)), m.width, m.height)
	}
	// The debug pane floats above any other overlay so it stays reachable.
	return m.debug.Overlay(view, m.width, m.height)
}

// headerView is two lines: the page name with a PAID badge and refresh
// state, and the running total with line count and refresh cadence.
func (m Model) headerView() string {
	title := ui.HeaderStyle().Render(ui.Icon("bill") + "Bill — " + m.label)
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
	title := "Costs"
	if m.summary {
		title = "Cost by service"
	}
	return ui.TablePanel(&m.tbl, true, title)
}

// footerView sits between the table and the status bar. It carries the two
// facts a shortened or rolled-up table must not leave unsaid: what the rows on
// screen add up to, and how many lines are being hidden. It is empty — and
// costs no height — when the table shows the bill as fetched.
//
// The total is of the rows shown, not bill.Total: under a filter or with
// zero-cost lines hidden those differ, and reporting the whole bill's total
// beneath a narrowed table would misdescribe what is on screen. When they do
// differ, the whole-bill figure is named alongside so neither number can be
// mistaken for the other.
func (m Model) footerView() string {
	if m.bill == nil {
		return ""
	}
	currency := m.bill.Currency

	var head string
	if m.summary {
		shown := totalOf(m.totals)
		text := fmt.Sprintf("%d service(s) · total %s",
			len(m.totals), billing.FormatAmount(shown, currency))
		if math.Abs(shown-m.bill.Total) > 0.0000005 {
			text += fmt.Sprintf(" of %s billed", billing.FormatAmount(m.bill.Total, currency))
		}
		head = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(ui.ColorText())).Render(text)
	}

	// What is hidden is always said out loud: the zero rows are hidden by
	// default, and a table quietly shorter than the bill would misreport it.
	var hints []string
	switch {
	case m.summary && m.hideZero && m.zeroSvc > 0:
		hints = append(hints, "hiding "+plural(m.zeroSvc, "service", "services")+" that cost nothing — z shows them")
	case m.hideZero && m.zeroCount > 0:
		hints = append(hints, fmt.Sprintf("hiding %d line(s) with no cost — z shows them", m.zeroCount))
	case m.zeroCount == 0:
	case m.summary:
		hints = append(hints, fmt.Sprintf("%d line(s) carry no cost — z hides them", m.zeroCount))
	default:
		hints = append(hints, fmt.Sprintf("%d line(s) carry no cost — z hides them, T summarises by service", m.zeroCount))
	}

	line := head
	if len(hints) > 0 {
		h := ui.MutedStyle().Render(strings.Join(hints, " · "))
		if line == "" {
			line = h
		} else {
			line += ui.MutedStyle().Render(" · ") + h
		}
	}
	if line == "" {
		return ""
	}
	return lipgloss.NewStyle().Padding(0, 1).Render(line)
}

func (m Model) statusBarView() string {
	// The active sort is shown by the arrow on the column header (see
	// rebuild), so the status bar only carries transient messages.
	return ui.StatusBar(m.width, m.status, m.keyHints())
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

	enter := ui.H("Enter", "detail")
	if m.summary {
		enter = ui.H("Enter", "lines")
	}
	hints := []ui.KeyHint{
		ui.H("↑/↓", "navigate"),
		enter,
		ui.H("x", "resources"),
		ui.H("u", "refresh"),
		ui.H("/", "filter"),
		ui.H("s", "sort"),
	}
	if m.sortCol > 0 {
		hints = append(hints, ui.H("R", "reverse"))
	}
	// The view toggles sit after the primary actions: the status bar elides
	// from the end, and dropping "sort" on a narrow terminal to make room for
	// them would trade a key people use for one they use occasionally.
	if m.summary {
		hints = append(hints, ui.H("T", "detail view"))
	} else {
		hints = append(hints, ui.H("T", "summary"))
	}
	if m.hideZero {
		hints = append(hints, ui.H("z", "show $0"))
	} else {
		hints = append(hints, ui.H("z", "hide $0"))
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

// billAboutText explains what the live bill TUI is for, shown in the About
// overlay ("i").
const billAboutText = "This is the live bill — your account's actual cost from the AWS Cost " +
	"Explorer API, grouped by service and usage type, with usage quantity and a " +
	"grand total. These are the Billing console's numbers, not the list-price " +
	"estimates the audit attaches to waste.\n\n" +
	"In --tui mode the screen re-fetches on a fixed interval; a Δ column shows " +
	"what each line moved since the last refresh. Press x for a per-resource " +
	"breakdown of a service, u to refresh now, / to filter and C to export.\n\n" +
	"A real bill arrives as hundreds of usage-type lines, most of them free, so " +
	"the rows that cost nothing start hidden and the footer says how many; z " +
	"shows them. Press T for one row per service with its total and share. Both " +
	"totals describe the rows on screen, so a filtered table never reports the " +
	"whole bill's figure.\n\n" +
	"Note: Cost Explorer is a paid API — every request (including each automatic " +
	"refresh) costs $0.01. Press ? for the full list of keyboard shortcuts."

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
		Width(w+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Padding(1, 2)
}

// overlayContentWidth is the usable width inside an overlay. Lip Gloss v2
// counts the border inside Width, so the frame takes its two columns out of
// the content — subtracting only the padding (as this did) leaves every line
// two columns too wide, which wraps the last word of a paragraph onto its own
// line and pushes a scrollbar gutter onto the line below the row it belongs to.
func (m Model) overlayContentWidth() int {
	style := m.overlayStyle()
	return style.GetWidth() - style.GetHorizontalPadding() - style.GetHorizontalBorderSize()
}

// detailRows returns the (label, value) pairs shown in the line detail overlay
// — the single source shared by the rendered overlay and the plain-text
// clipboard copy. The Change row only appears when there's a delta to show.
func (m Model) detailRows(l *billing.Line) [][2]string {
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}
	rows := [][2]string{
		{"Usage type", l.UsageType},
		{"Usage", billing.FormatQty(l.Quantity) + " " + l.Unit},
		{"Cost", fmt.Sprintf("%s (%.6f %s)", billing.FormatAmount(l.Amount, currency), l.Amount, currency)},
	}
	if d := m.deltas[l.Key()]; d != 0 {
		rows = append(rows, [2]string{"Change", formatDelta(d, currency)})
	}
	rows = append(rows, [2]string{"Period", m.start.Format("2006-01-02") + " → " + m.end.Format("2006-01-02")})
	return rows
}

func (m Model) detailOverlay() string {
	l := m.selected()
	if l == nil {
		return ""
	}
	style := m.overlayStyle()
	w := m.overlayContentWidth()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(12)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 12)

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(l.Service) + "\n\n")
	for _, r := range m.detailRows(l) {
		v := r[1]
		if v == "" {
			v = "-"
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Render(r[0]), value.Render(v)) + "\n")
	}
	b.WriteString("\n" + ui.MutedStyle().Render("x lists this service's resources · y copies this line · Esc closes"))
	return style.Render(b.String())
}

// detailText renders the selected line as plain, unstyled text for the
// clipboard, so the overlay can be pasted without ANSI escapes or the rest of
// the table coming along.
func (m Model) detailText(l *billing.Line) string {
	var b strings.Builder
	b.WriteString(l.Service + "\n\n")
	for _, r := range m.detailRows(l) {
		v := r[1]
		if v == "" {
			v = "-"
		}
		b.WriteString(r[0] + ": " + v + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// resNote is the standing caveat on the resource breakdown: Cost Explorer only
// keeps resource-level data for a trailing window, so these totals are not the
// service's whole bill.
const resWindowDays = 14

// resHeader is the overlay's fixed top block. It is built separately from the
// body so its height can be measured rather than guessed — the note wraps to
// one line or two depending on the terminal's width, and assuming either is
// how an overlay ends up taller than the screen.
func (m Model) resHeader(w int) string {
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}
	var shown float64
	for _, r := range m.resRows {
		shown += r.Amount
	}

	// Three separate facts, so three bullets. Run together as a paragraph they
	// wrapped into a block of grey that reads as boilerplate — and the second
	// one, that this window is not the whole bill, is the one that stops the
	// smaller total looking like an error.
	var points []string
	if len(m.resRows) > 0 {
		points = append(points, fmt.Sprintf("%s across %s in the last %d days (%s → %s).",
			billing.FormatAmount(shown, currency),
			plural(len(m.resRows), "resource", "resources"),
			resWindowDays, m.resStart.Format("2006-01-02"), m.end.Format("2006-01-02")))
		points = append(points, fmt.Sprintf("This service is %s on the bill, which covers %s → %s.",
			billing.FormatAmount(m.serviceTotal(m.resService), currency),
			m.start.Format("2006-01-02"), m.end.Format("2006-01-02")))
	}
	points = append(points, fmt.Sprintf(
		"This table reflects the last %d days only — Cost Explorer keeps resource-level data no further back.",
		resWindowDays))

	out := ui.HeaderStyle().Render("Resources — " + m.resService)
	for _, p := range points {
		out += "\n" + bulletLine(p, w)
	}
	return out
}

// bulletLine renders one bullet, wrapped to w with the continuation lines
// indented under the text rather than under the marker — so a wrapped bullet
// still reads as one point.
func bulletLine(text string, w int) string {
	const marker = "• "
	body := ui.MutedStyle().Width(w - len([]rune(marker))).Render(text)
	lines := strings.Split(body, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = ui.MutedStyle().Render(marker) + lines[i]
			continue
		}
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// serviceTotal is what one service costs over the whole billing period — the
// figure the summary shows, which the resource window's total is compared
// against.
func (m Model) serviceTotal(service string) float64 {
	var sum float64
	if m.bill == nil {
		return 0
	}
	for _, l := range m.bill.Lines {
		if l.Service == service {
			sum += l.Amount
		}
	}
	return sum
}

// resVisibleRows is how many resource rows fit in the overlay. The view and the
// key handler both need it — the view to window the list, the handler to know
// where scrolling stops — and they must agree, or the last page scrolls into
// empty space.
func (m Model) resVisibleRows() int {
	w := m.overlayContentWidth()
	// Overlay chrome: the border (2) and the style's vertical padding (2),
	// then the header, the blank line under it and the footer line, and two
	// rows of breathing room so the panel never runs to the screen edge.
	chrome := 4 + lipgloss.Height(m.resHeader(w)) + 2 + 2
	n := m.height - chrome
	if n < 3 {
		n = 3
	}
	return n
}

// resMaxScroll is the last offset that still fills the panel. Scrolling past it
// would leave a short, ragged list at the bottom — and, when the panel sizes
// itself to its content, a box that visibly shrinks as you press down.
func (m Model) resMaxScroll() int {
	if max := len(m.resRows) - m.resVisibleRows(); max > 0 {
		return max
	}
	return 0
}

func (m Model) resourcesOverlay() string {
	style := m.overlayStyle()
	w := m.overlayContentWidth()
	currency := ""
	if m.bill != nil {
		currency = m.bill.Currency
	}
	rows := m.resVisibleRows()

	var b strings.Builder
	b.WriteString(m.resHeader(w) + "\n\n")

	// Every state renders the same number of body lines, so the panel keeps
	// one size: switching between "fetching", an error and the list, or
	// scrolling to the end of a short page, must not resize the overlay under
	// the reader's cursor.
	pad := func(block string, footer string) {
		lines := strings.Split(block, "\n")
		for len(lines) < rows {
			lines = append(lines, "")
		}
		b.WriteString(strings.Join(lines[:rows], "\n"))
		b.WriteString("\n" + footer)
	}

	switch {
	case m.resFetching:
		pad(m.spin.View()+" fetching resource costs…", ui.MutedStyle().Render("Esc closes"))
	case m.resErr != "":
		pad(ui.ErrorStyle().Width(w).Render("⚠ "+m.resErr), ui.MutedStyle().Render("Esc closes"))
	case len(m.resRows) == 0:
		pad(ui.MutedStyle().Render("No resource-level cost recorded for this service in the window."),
			ui.MutedStyle().Render("Esc closes"))
	default:
		start := m.resScroll
		if max := m.resMaxScroll(); start > max {
			start = max
		}
		if start < 0 {
			start = 0
		}
		end := start + rows
		if end > len(m.resRows) {
			end = len(m.resRows)
		}

		// The scrollbar's column is reserved whether or not it has a thumb
		// (VScrollbar returns a blank gutter when everything fits), so the
		// rows do not reflow the moment the list grows past one page.
		bodyW := w - 2
		amount := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(12)
		var list []string
		for _, r := range m.resRows[start:end] {
			list = append(list, lipgloss.JoinHorizontal(lipgloss.Top,
				amount.Render(billing.FormatAmount(r.Amount, currency)),
				lipgloss.NewStyle().Width(bodyW-12).MaxWidth(bodyW-12).Render(
					fmt.Sprintf("%s  (%s %s)", r.Resource, billing.FormatQty(r.Quantity), r.Unit))))
		}
		for len(list) < rows {
			list = append(list, lipgloss.NewStyle().Width(bodyW).Render(""))
		}
		body := lipgloss.JoinHorizontal(lipgloss.Top,
			strings.Join(list[:rows], "\n"),
			" ",
			ui.VScrollbar(rows, len(m.resRows), rows, start))

		b.WriteString(body)
		b.WriteString("\n" + ui.MutedStyle().Render(fmt.Sprintf(
			"%d–%d of %d · ↑/↓ PgUp/PgDn Home/End scroll · Esc closes",
			start+1, end, len(m.resRows))))
	}
	return style.Render(b.String())
}

func (m Model) helpOverlay() string {
	style := m.overlayStyle()
	rows := []struct{ key, action string }{
		{"↑/↓, j/k", "Navigate bill lines"},
		{"Enter", "Open the detail overlay for the selected line (in the summary: filter down to that service's lines)"},
		{"T", "Toggle the per-service summary — one row per service with its total and share of the bill"},
		{"z", "Show / hide the rows that cost nothing (hidden by default)"},
		{"x", "Per-resource breakdown for the selected service (needs resource-level data enabled)"},
		{"u", "Refresh now (PAID — one $0.01 Cost Explorer request)"},
		{"/", "Quick filter (matches service, usage type, unit)"},
		{"s / R", "Sort by the next column / reverse the direction"},
		{"r", "Reset filter and sort"},
		{"</> or ,/.", "Scroll columns when the table is wider than the screen"},
		{"y", "Copy the selected service and usage type (or the whole detail panel when it's open)"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"~", "Debug: live view of what the tool is doing"},
		{"i", "About this page (what it does)"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	sort.SliceStable(rows, func(i, j int) bool { return ui.SortKeyLess(rows[i].key, rows[j].key) })
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("Bill — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	b.WriteString("\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Bold(true).
		Render("PAID FEATURE: Cost Explorer bills $0.01 per request. ") +
		ui.MutedStyle().Render("Every automatic refresh at the configured --interval is one such request; the CHANGE column shows what moved since the previous refresh."))
	return style.Render(b.String())
}

// View renders the frame for Bubble Tea v2. The terminal modes (alt screen,
// mouse, window title) are declared by the application shell that wraps every
// TUI (ui.WithWindowTitle); tests read the frame via View().Content.
func (m Model) View() tea.View { return tea.NewView(m.viewString()) }
