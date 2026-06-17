package laketui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

const chromeHeight = 2 /* panel border */ + 1 /* scroll hint */ + 1 /* status bar */

func (m *Model) layoutTable() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.tbl.SetWidth(m.width - 4)
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

	view := lipgloss.JoinVertical(lipgloss.Left, m.headerView(), m.bodyView(), m.statusBarView())
	view = ui.ClipToSize(view, m.width, m.height)

	switch m.overlay {
	case overlayDetail:
		view = ui.OverlayCenterBlank(m.detailOverlay(), m.width, m.height)
	case overlayHelp:
		view = ui.OverlayCenterBlank(m.helpOverlay(), m.width, m.height)
	case overlayAbout:
		view = ui.OverlayCenterBlank(ui.AboutView("About — CloudTrail Lake", lakeAboutText, ui.AboutWidth(m.width)), m.width, m.height)
	}
	return m.debug.Overlay(view, m.width, m.height)
}

func (m Model) headerView() string {
	title := ui.HeaderStyle().Render("CloudTrail Lake")

	var status string
	switch {
	case m.loading:
		status = ui.MutedStyle().Render(m.spin.View() + " running query…")
	case m.loadErr != nil:
		status = ui.ErrorStyle().Render("✗ query failed")
	default:
		status = ui.MutedStyle().Render(m.title)
	}
	line1 := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", status)
	if badge := ui.RegionBadge([]string{m.region}, false); badge != "" {
		line1 = lipgloss.JoinHorizontal(lipgloss.Top, line1, "  ", badge)
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("store %s", m.store))
	if !m.loading && m.loadErr == nil {
		parts = append(parts, fmt.Sprintf("%d row(s)", len(m.allRows)))
		if m.result.BytesScanned > 0 {
			parts = append(parts, "scanned "+humanBytes(m.result.BytesScanned))
		}
	}
	line2 := ui.MutedStyle().Render(strings.Join(parts, " · "))
	if q := m.filterIn.Value(); m.filtering || q != "" {
		line2 += "  " + m.filterIn.View() +
			ui.MutedStyle().Render(fmt.Sprintf(" %d/%d", len(m.visible), len(m.allRows)))
	}
	return line1 + "\n" + line2
}

func (m Model) bodyView() string {
	if m.loadErr != nil {
		msg := ui.ErrorStyle().Render("✗ "+m.loadErr.Error()) + "\n\n" +
			ui.MutedStyle().Render("CloudTrail Lake needs cloudtrail:StartQuery and cloudtrail:GetQueryResults on the event data store.")
		return lipgloss.NewStyle().Padding(1, 2).Render(msg)
	}
	if !m.loading && len(m.allRows) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			ui.MutedStyle().Render("The query returned no rows."))
	}
	if !m.loading && len(m.visible) == 0 {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			ui.MutedStyle().Render("No rows match the current filter."))
	}
	if m.loading {
		return lipgloss.NewStyle().Padding(1, 2).Render(
			ui.MutedStyle().Render(m.spin.View() + " waiting for CloudTrail Lake…"))
	}
	panel := ui.TablePanelStyle(true).Render(m.tbl.View())
	hint := ui.TableScrollIndicator(&m.tbl)
	return panel + "\n" + hint
}

func (m Model) statusBarView() string {
	return ui.StatusBar(m.width, m.status, m.keyHints())
}

func (m Model) keyHints() []ui.KeyHint {
	if m.filtering {
		return []ui.KeyHint{ui.H("Enter", "keep filter"), ui.H("Esc", "clear")}
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
		ui.H("~", "debug"),
		ui.H("i", "about"),
		ui.H("?", "help"),
		ui.H("q", "quit"),
	)
	return hints
}

// lakeAboutText explains what the CloudTrail Lake TUI is for, shown in the
// About overlay ("i").
const lakeAboutText = "This explores the results of a CloudTrail Lake SQL query. Where the trail " +
	"feed covers 90 days of management events, a Lake event data store can hold " +
	"years of history and data events (S3 object access, Lambda invokes, …) and " +
	"supports aggregation.\n\n" +
	"The query (a built-in --top-principals/--top-events, or your own --sql) and " +
	"store are set by the command's flags; this screen makes the returned rows " +
	"navigable. Press Enter for a labelled per-row detail overlay, / to filter, " +
	"s/R for numeric-aware sort, y to copy and C to export.\n\n" +
	"Press ? for the full list of keyboard shortcuts."

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

// detailRows returns the (column, value) pairs for the selected row — the
// single source shared by the rendered overlay and the plain-text clipboard
// copy. Empty cells fall back to "-".
func (m Model) detailRows() [][2]string {
	row := m.selected()
	if row == nil {
		return nil
	}
	rows := make([][2]string, len(m.result.Columns))
	for i, name := range m.result.Columns {
		v := "-"
		if i < len(*row) && (*row)[i] != "" {
			v = (*row)[i]
		}
		rows[i] = [2]string{name, v}
	}
	return rows
}

func (m Model) detailOverlay() string {
	if m.selected() == nil {
		return ""
	}
	style := m.overlayStyle()
	w := style.GetWidth() - style.GetHorizontalPadding()

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Width(20)
	value := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Width(w - 20)

	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("Row detail") + "\n\n")
	for _, r := range m.detailRows() {
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, label.Render(r[0]), value.Render(r[1])) + "\n")
	}
	b.WriteString("\n" + ui.MutedStyle().Render("y copies this row · Esc closes"))
	return style.Render(b.String())
}

// detailText renders the selected row as plain, unstyled "column: value" lines
// for the clipboard, so the overlay can be pasted without ANSI escapes or the
// rest of the table coming along.
func (m Model) detailText() string {
	rows := m.detailRows()
	if rows == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("Row detail\n\n")
	for _, r := range rows {
		b.WriteString(r[0] + ": " + r[1] + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) helpOverlay() string {
	style := m.overlayStyle()
	rows := []struct{ key, action string }{
		{"↑/↓, j/k", "Navigate rows"},
		{"Enter", "Open the detail overlay for the selected row"},
		{"/", "Quick filter (matches any column, live matched/total count)"},
		{"s / R", "Sort by the next column / reverse (numeric-aware)"},
		{"r", "Reset filter and sort"},
		{"</> or ,/.", "Scroll columns when the table is wider than the screen"},
		{"y", "Copy the selected row (tab-separated, or the labelled detail panel when it's open)"},
		{"C", "Export the current view to CSV under ~/.aws_explorer/exports/"},
		{"~", "Debug: live view of what the tool is doing"},
		{"i", "About this page (what it does)"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}
	sort.SliceStable(rows, func(i, j int) bool { return ui.SortKeyLess(rows[i].key, rows[j].key) })
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Width(12)
	var b strings.Builder
	b.WriteString(ui.HeaderStyle().Render("CloudTrail Lake — keys") + "\n\n")
	for _, r := range rows {
		b.WriteString(keyStyle.Render(r.key) + r.action + "\n")
	}
	return style.Render(b.String())
}

// humanBytes renders a byte count compactly (KB/MB/GB) for the scan stat.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
