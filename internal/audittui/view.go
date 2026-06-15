package audittui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/costs"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// chromeHeight is the height of everything below the header: the table panel's
// top and bottom borders, the column-scroll hint, and the status bar. The
// header's height is measured separately (it varies — title, scan progress,
// tally — and ui.HeaderStyle adds a bottom margin), because under-counting it
// makes the frame too tall and ClipToSize trims the status bar off the bottom.
const chromeHeight = 2 /* panel border */ + 1 /* scroll hint */ + 1 /* status bar */

// layoutTable resizes the findings table to the current terminal.
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
	case overlayErrors:
		view = ui.OverlayCenter(view, m.errorsOverlay(), m.width, m.height)
	case overlayHelp:
		view = ui.OverlayCenter(view, m.helpOverlay(), m.width, m.height)
	case overlayAbout:
		view = ui.OverlayCenter(view, ui.AboutView("About — Cost Audit", auditAboutText, ui.AboutWidth(m.width)), m.width, m.height)
	}
	// The debug pane floats above any other overlay so it stays reachable.
	return m.debug.Overlay(view, m.width, m.height)
}

// headerView is two lines: the page name with scan progress, and the running
// tally (findings, severities, total estimated savings, error badge).
func (m Model) headerView() string {
	title := ui.HeaderStyle().Render("Cost audit")

	var progress string
	if m.scanning {
		progress = ui.MutedStyle().Render(fmt.Sprintf("%s scanning regions %d/%d", m.spin.View(), m.scanned, m.regions))
	} else {
		progress = ui.MutedStyle().Render(fmt.Sprintf("scanned %d region(s)", m.scanned))
	}
	// JoinHorizontal (not string concat) so progress sits on the title's first
	// row; ui.HeaderStyle's bottom margin would otherwise drop it a line.
	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", progress)
	// Spotlight the scanned region scope unless --all-regions widened it.
	if badge := ui.RegionBadge(m.regionNames, m.allRegions); badge != "" {
		line1 = lipgloss.JoinHorizontal(lipgloss.Top, line1, "  ", badge)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("%d finding(s)", len(m.all)))
	if len(m.all) > 0 {
		parts = append(parts, findings.Summary(m.all))
		if total := findings.TotalMonthlyUSD(m.all); total > 0 {
			parts = append(parts, "potential savings ≈ "+costs.Dollars(total)+"/month")
		}
	}
	line2 := ui.MutedStyle().Render(strings.Join(parts, " · "))
	if n := len(m.errs); n > 0 {
		line2 += "  " + ui.ErrorStyle().Render(fmt.Sprintf("⚠ %d error(s)", n)) +
			ui.MutedStyle().Render(" (press e)")
	}
	if q := m.filter.Value(); m.filtering || q != "" {
		line2 += "  " + m.filter.View() +
			ui.MutedStyle().Render(fmt.Sprintf(" %d/%d", len(m.visible), len(m.all)))
	}
	return line1 + "\n" + line2
}

func (m Model) bodyView() string {
	if !m.scanning && len(m.all) == 0 {
		msg := ui.SuccessStyle().Render("✓ No cost waste found.")
		if len(m.errs) > 0 {
			msg += ui.MutedStyle().Render("  (some checks were skipped — press e for errors)")
		}
		return lipgloss.NewStyle().Padding(1, 2).Render(msg)
	}
	panel := ui.TablePanelStyle(true).Render(m.tbl.View())
	hint := ui.TableScrollIndicator(&m.tbl)
	return panel + "\n" + hint
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
	if m.overlay == overlayErrors {
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
		ui.H("/", "filter"),
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
	)
	if len(m.errs) > 0 {
		hints = append(hints, ui.H("e", "errors"))
	}
	hints = append(hints, ui.H("~", "debug"), ui.H("i", "about"), ui.H("?", "help"), ui.H("q", "quit"))
	return hints
}

// auditAboutText explains what the audit TUI is for, shown in the About
// overlay ("i").
const auditAboutText = "This is the cost & posture audit. It scans your configured regions for " +
	"findings across cost/waste, security, IAM hygiene, messaging and CloudTrail " +
	"checks, and ranks them by severity. Each cost finding carries an estimated " +
	"monthly saving, with a running total in the header.\n\n" +
	"The table fills in as each region completes. Press Enter on a finding for its " +
	"full explanation, fix and estimate; / filters, s/R sort, y copies the ARN, " +
	"and C exports the current view to CSV.\n\n" +
	"Every finding has a stable check ID (e.g. COST-EBS-001) safe to reference in " +
	"runbooks. Press ? for the full list of keyboard shortcuts."

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

// detailRows returns the labelled (name, value) pairs shown below the finding's
// title — the single source shared by the rendered overlay and the plain-text
// clipboard copy. The ARN row only appears when the finding carries one.
func detailRows(f *findings.Finding) [][2]string {
	rows := [][2]string{
		{"Resource", f.Resource},
		{"Region", f.Region},
		{"Service", f.Service},
	}
	if f.ARN != "" {
		rows = append(rows, [2]string{"ARN", f.ARN})
	}
	return append(rows,
		[2]string{"Est/month", costs.Dollars(f.EstMonthlyUSD)},
		[2]string{"Why", f.Detail},
		[2]string{"Fix", f.Fix},
	)
}

// blockRow names the rows of remediation prose that get a trailing blank line,
// setting them apart from the finding's identity.
func blockRow(name string) bool {
	return name == "Est/month" || name == "Why" || name == "Fix"
}

func (m Model) detailOverlay() string {
	f := m.selected()
	if f == nil {
		return ""
	}
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(10)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 10)

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(f.Severity.Badge()+"  "+f.ID) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Width(w).Render(f.Title) + "\n\n")
	for _, r := range detailRows(f) {
		v := r[1]
		if v == "" {
			v = "-"
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Render(r[0]), value.Render(v)) + "\n")
		if blockRow(r[0]) {
			b.WriteString("\n")
		}
	}
	b.WriteString(ui.MutedStyle().Render("y copies these details · Esc closes"))
	return style.Render(b.String())
}

// detailText renders the finding as plain, unstyled text for the clipboard, so
// the overlay can be pasted into a ticket or message without ANSI escapes or
// the rest of the table coming along.
func detailText(f *findings.Finding) string {
	var b strings.Builder
	b.WriteString(f.Severity.String() + "  " + f.ID + "\n")
	b.WriteString(f.Title + "\n\n")
	for _, r := range detailRows(f) {
		v := r[1]
		if v == "" {
			v = "-"
		}
		b.WriteString(r[0] + ": " + v + "\n")
		if blockRow(r[0]) {
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) errorsOverlay() string {
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	// Window the list around the scroll position so long error lists fit.
	maxLines := m.height - 12
	if maxLines < 4 {
		maxLines = 4
	}
	start := m.errScroll
	if start > len(m.errs)-1 {
		start = len(m.errs) - 1
	}
	end := start + maxLines
	if end > len(m.errs) {
		end = len(m.errs)
	}

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(fmt.Sprintf("Collection errors (%d)", len(m.errs))) + "\n")
	b.WriteString(ui.MutedStyle().Width(w).Render("Each error skipped only the checks that need that data; everything else was audited.") + "\n\n")
	for _, e := range m.errs[start:end] {
		line := fmt.Sprintf("• %s@%s: %s — %s", e.Service, e.Region, e.Code, e.Message)
		b.WriteString(lipgloss.NewStyle().Width(w).Render(line) + "\n")
	}
	if end < len(m.errs) {
		b.WriteString(ui.MutedStyle().Render(fmt.Sprintf("… %d more (↓ to scroll)", len(m.errs)-end)))
	}
	return style.Render(b.String())
}

func (m Model) helpOverlay() string {
	style := m.overlayStyle()
	rows := []struct{ key, action string }{
		{"↑/↓, j/k", "Navigate findings"},
		{"Enter", "Open the detail overlay for the selected finding"},
		{"/", "Quick filter (matches any field, live matched/total count)"},
		{"s / R", "Sort by the next column / reverse the direction"},
		{"r", "Reset filter and sort"},
		{"</> or ,/.", "Scroll columns when the table is wider than the screen"},
		{"y", "Copy the finding's ARN/resource ID (or the whole detail panel when it's open)"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"e", "Show collection errors"},
		{"~", "Debug: live view of what the tool is doing"},
		{"i", "About this page (what it does)"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("Cost audit — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	return style.Render(b.String())
}
