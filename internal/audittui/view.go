package audittui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/costs"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// headerLines + the panel border/padding rows the table doesn't get.
const chromeHeight = 2 /* header */ + 2 /* panel border */ + 1 /* scroll hint */ + 1 /* status bar */

// layoutTable resizes the findings table to the current terminal.
func (m *Model) layoutTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.tbl.SetWidth(m.width - 4) // panel border + padding
	h := m.height - chromeHeight
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
	case overlayErrors:
		return ui.OverlayCenter(view, m.errorsOverlay(), m.width, m.height)
	case overlayHelp:
		return ui.OverlayCenter(view, m.helpOverlay(), m.width, m.height)
	}
	return view
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
	line1 := title + "  " + progress

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
	left := m.status
	if left == "" {
		left = m.sortLabel()
	}
	return ui.StatusBar(m.width, left, m.keyHints())
}

// sortLabel names the active sort for the status bar.
func (m Model) sortLabel() string {
	if m.sortCol < 0 {
		return "sorted by severity & cost"
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
	if m.sortCol >= 0 {
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
	hints = append(hints, ui.H("?", "help"), ui.H("q", "quit"))
	return hints
}

// overlayStyle is the shared frame for all overlays.
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
	f := m.selected()
	if f == nil {
		return ""
	}
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(10)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 10)
	row := func(name, v string) string {
		if v == "" {
			v = "-"
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, label.Render(name), value.Render(v))
	}

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render(f.Severity.Badge()+"  "+f.ID) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Width(w).Render(f.Title) + "\n\n")
	b.WriteString(row("Resource", f.Resource) + "\n")
	b.WriteString(row("Region", f.Region) + "\n")
	b.WriteString(row("Service", f.Service) + "\n")
	if f.ARN != "" {
		b.WriteString(row("ARN", f.ARN) + "\n")
	}
	b.WriteString(row("Est/month", costs.Dollars(f.EstMonthlyUSD)) + "\n\n")
	b.WriteString(row("Why", f.Detail) + "\n\n")
	b.WriteString(row("Fix", f.Fix) + "\n\n")
	b.WriteString(ui.MutedStyle().Render("y copies the ARN/resource · Esc closes"))
	return style.Render(b.String())
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
		{"y", "Copy the selected finding's ARN (or resource ID)"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"e", "Show collection errors"},
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
