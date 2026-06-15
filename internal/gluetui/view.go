package gluetui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

func (mm *m) View() string {
	if mm.err != nil {
		return mm.renderError()
	}

	var sb strings.Builder
	if badge := ui.RegionBadge(mm.regions, mm.allRegions); badge != "" {
		sb.WriteString(badge + "\n")
	}

	if mm.runsActive {
		sb.WriteString(mm.renderRuns())
	} else {
		sb.WriteString(mm.renderTabBar() + "\n")
		sb.WriteString(mm.renderTable())
	}

	sb.WriteString("\n" + ui.StatusBar(mm.width, mm.statusLeft(), mm.helpHints()))

	frame := mm.applyToast(sb.String())
	if mm.defActive {
		title := "Job — " + mm.def.Name
		if mm.defLoading {
			title = "Job definition"
		}
		frame = ui.OverlayCenter(frame, ui.AboutView(title, mm.defBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.showAbout {
		frame = ui.OverlayCenter(frame, ui.AboutView("About — AWS Glue", glueAboutText, ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	return frame
}

// defBody renders the job-definition overlay's contents (loading / error /
// detail). Kept pure over the model's def state so it is straightforward to read.
func (mm *m) defBody() string {
	if mm.defLoading {
		return mm.spinner.View() + " Loading job definition…"
	}
	if mm.defErr != nil {
		return "Could not load definition: " + mm.defErr.Error()
	}
	d := mm.def
	var b strings.Builder
	row := func(label, value string) {
		if value == "" {
			value = "—"
		}
		b.WriteString(fmt.Sprintf("%-16s %s\n", label, value))
	}
	row("Role", d.Role)
	row("Glue version", d.GlueVersion)
	row("Execution class", d.ExecutionClass)
	row("Worker", d.Worker)
	row("Timeout", fmt.Sprintf("%d min", d.TimeoutMinutes))
	row("Max retries", fmt.Sprintf("%d", d.MaxRetries))
	row("Job bookmark", map[bool]string{true: "enabled", false: "disabled"}[d.BookmarkEnabled])
	row("Script", d.Script)
	row("Connections", strings.Join(d.Connections, ", "))
	row("Security config", d.SecurityConfig)
	if len(d.DefaultArguments) > 0 {
		b.WriteString("\nDefault arguments (secrets redacted):\n")
		for _, k := range sortedKeys(d.DefaultArguments) {
			b.WriteString(fmt.Sprintf("  %s = %s\n", k, d.DefaultArguments[k]))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

const glueAboutText = "This is the AWS Glue dashboard. Tab across Jobs, Crawlers, Triggers, " +
	"Workflows, Connections and the Catalog (databases); each row shows health " +
	"at a glance — a job's last run state and duration, a crawler's last-crawl " +
	"status.\n\n" +
	"Press Enter on a job to see its run history: state, duration, DPU-hours and " +
	"an estimated cost per run, with the error message inline on failures. In the " +
	"run history, L opens that run's CloudWatch logs.\n\n" +
	"Press o on any row to open it in the AWS console, / to filter, and r to refresh."

func (mm *m) renderTabBar() string {
	var parts []string
	for t := tab(0); t < tabCount; t++ {
		label := fmt.Sprintf(" %s (%d) ", tabNames[t], len(mm.rowsForTab(t)))
		if t == mm.tab {
			parts = append(parts, lipgloss.NewStyle().
				Background(lipgloss.Color(ui.ColorHighlight())).
				Foreground(lipgloss.Color(ui.ColorHighlightText())).
				Bold(true).Render(label))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(lipgloss.Color(ui.ColorText())).Render(label))
		}
	}
	return "Glue ▸ " + strings.Join(parts, " ")
}

func (mm *m) renderTable() string {
	specs, _ := mm.specsAndRows(mm.tab)
	rows := mm.currentRows()
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder

	// Filter line.
	if mm.filterActive {
		b.WriteString(" " + mm.filter.View() + "\n")
	} else if v := mm.filter.Value(); v != "" {
		b.WriteString("  filter: " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(v) + "  (/ to edit)\n")
	} else {
		b.WriteString("  (/ to filter)\n")
	}
	b.WriteString("\n")

	// Header.
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(specs, widths)) + "\n")

	if mm.loading {
		b.WriteString(fmt.Sprintf("  %s Loading Glue resources…\n", mm.spinner.View()))
	} else if len(rows) == 0 {
		b.WriteString("  No " + strings.ToLower(tabNames[mm.tab]) + " found in scope.\n")
	} else {
		visible := mm.height - 9
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.sel[mm.tab], len(rows), visible)
		for i := start; i < end; i++ {
			b.WriteString(renderRow(rows[i].cells, widths, i == mm.sel[mm.tab]) + "\n")
		}
	}

	// One row shorter than the other panels: this view also carries the tab bar
	// above the box, so the panel must leave a line for the status bar below it.
	return boxStyle(mm.width, mm.height-5).Render(b.String())
}

func (mm *m) renderRuns() string {
	specs := []colSpec{{"STARTED", 16}, {"STATE", 14}, {"DURATION", 10}, {"DPU-HRS", 8}, {"EST", 8}, {"WORKER", 12}, {"ATTEMPT", 7}}
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" Runs — %s [%s]", mm.runsJob.Name, mm.runsJob.Region)) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(specs, widths)) + "\n")

	switch {
	case mm.runsLoading:
		b.WriteString(fmt.Sprintf("  %s Loading run history…\n", mm.spinner.View()))
	case mm.runsErr != nil:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load runs: "+mm.runsErr.Error()) + "\n")
	case len(mm.runs) == 0:
		b.WriteString("  No runs recorded for this job.\n")
	default:
		visible := mm.height - 12
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.runsSel, len(mm.runs), visible)
		for i := start; i < end; i++ {
			r := mm.runs[i]
			cells := []cell{
				{text: shortTime(r.Started)},
				{text: stateLabel(r.State), color: stateColor(r.State)},
				{text: formatDuration(r.ExecSecs)},
				{text: formatDPUHours(r.DPUSeconds)},
				{text: formatCost(r.DPUSeconds)},
				{text: r.Worker},
				{text: fmt.Sprintf("%d", r.Attempt)},
			}
			b.WriteString(renderRow(cells, widths, i == mm.runsSel) + "\n")
		}

		// Error detail for the selected run.
		if mm.runsSel < len(mm.runs) && mm.runs[mm.runsSel].Error != "" {
			b.WriteString("\n")
			errText := truncate(mm.runs[mm.runsSel].Error, contentW-4)
			b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
				Render("✗ "+errText) + "\n")
		}

		// Footer totals.
		dpu, cost := runsTotals(mm.runs)
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
			Render(fmt.Sprintf("%d runs · %.2f DPU-hrs ≈ $%.2f (estimate)", len(mm.runs), dpu, cost)) + "\n")
	}

	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

func (mm *m) renderError() string {
	b := "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).
		Render("  AWS Glue dashboard error") + "\n\n"
	b += fmt.Sprintf("  %v\n\n", mm.err)
	b += "  Enter/Esc — retry · q — quit\n"
	return boxStyle(mm.width, mm.height-4).
		BorderForeground(lipgloss.Color(ui.ColorError())).Render(b)
}

func boxStyle(width, height int) lipgloss.Style {
	if height < 3 {
		height = 3
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorder())).
		Width(width - 4).
		Height(height)
}

func (mm *m) statusLeft() string {
	if mm.runsActive {
		return fmt.Sprintf("Job: %s  ·  Runs: %d", mm.runsJob.Name, len(mm.runs))
	}
	regionLabel := mm.regions[0]
	if len(mm.regions) != 1 {
		regionLabel = fmt.Sprintf("all (%d regions)", len(mm.regions))
	}
	return fmt.Sprintf("Region: %s  ·  %s: %d", regionLabel, tabNames[mm.tab], mm.rowCount())
}

func (mm *m) helpHints() []ui.KeyHint {
	if mm.runsActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "runs"),
			ui.H("L", "logs"),
			ui.H("y", "copy error"),
			ui.H("Esc", "back"),
			ui.H("i", "about"),
			ui.H("q", "quit"),
		}
	}
	hints := []ui.KeyHint{
		ui.H("Tab", "pane"),
		ui.H("↑/↓", "rows"),
	}
	if mm.tab == tabJobs {
		hints = append(hints, ui.H("Enter", "runs"), ui.H("d", "definition"))
	}
	hints = append(hints,
		ui.H("/", "filter"),
		ui.H("o", "console"),
		ui.H("r", "refresh"),
		ui.H("i", "about"),
		ui.H("q", "quit"),
	)
	return hints
}

func (mm *m) applyToast(rendered string) string {
	if mm.toast == "" {
		return rendered
	}
	toast := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorSuccess())).
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Padding(0, 2).Bold(true).Render("✓ " + mm.toast)
	lines := strings.Split(rendered, "\n")
	if len(lines) >= 1 {
		lines[0] = lipgloss.PlaceHorizontal(mm.width, lipgloss.Right, toast)
	}
	return strings.Join(lines, "\n")
}

// --- pure table layout helpers ---------------------------------------------

// resolveWidths turns column specs into concrete widths: fixed columns keep
// their width, the single flex column (width 0) absorbs the remainder (down to
// a floor), accounting for one space between columns.
func resolveWidths(specs []colSpec, total int) []int {
	n := len(specs)
	widths := make([]int, n)
	if n == 0 {
		return widths
	}
	gaps := n - 1
	// Budget for column text: the row total minus the inter-column gaps and the
	// single leading space every header/row is prefixed with. Reserving it keeps
	// a full-width row from spilling one cell past the panel and wrapping.
	budget := total - gaps - 1
	if budget < n {
		budget = n
	}

	flex := -1
	used := 0
	for i, s := range specs {
		if s.width == 0 && flex == -1 {
			flex = i
			continue
		}
		widths[i] = s.width
		used += s.width
	}
	if flex >= 0 {
		w := budget - used
		if w < 8 {
			w = 8
		}
		widths[flex] = w
		used += w
	}

	// When the fixed columns alone overrun the budget (narrow terminals), shrink
	// the widest column one cell at a time until the row fits. This trades a
	// little truncation for a table that never wraps fields onto the next line.
	for used > budget {
		wi := 0
		for i := 1; i < n; i++ {
			if widths[i] > widths[wi] {
				wi = i
			}
		}
		if widths[wi] <= 1 {
			break
		}
		widths[wi]--
		used--
	}
	return widths
}

func headerLine(specs []colSpec, widths []int) string {
	parts := make([]string, len(specs))
	for i, s := range specs {
		parts[i] = pad(truncate(s.title, widths[i]), widths[i])
	}
	return " " + strings.Join(parts, " ")
}

// renderRow lays cells into fixed-width columns. A selected row is painted with
// the highlight background (overriding per-cell colours); otherwise each cell
// keeps its own colour.
func renderRow(cells []cell, widths []int, selected bool) string {
	parts := make([]string, len(widths))
	for i := range widths {
		text := ""
		color := ""
		if i < len(cells) {
			text = cells[i].text
			color = cells[i].color
		}
		field := pad(truncate(text, widths[i]), widths[i])
		if !selected && color != "" {
			field = lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(field)
		}
		parts[i] = field
	}
	line := " " + strings.Join(parts, " ")
	if selected {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(ui.ColorHighlight())).
			Foreground(lipgloss.Color(ui.ColorHighlightText())).
			Render(line)
	}
	return line
}

// pad right-pads s with spaces to width (s is assumed already truncated).
func pad(s string, width int) string {
	n := width - len([]rune(s))
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}

func visibleRange(current, total, maxVisible int) (int, int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start := current - half
	if start < 0 {
		start = 0
	}
	end := start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}
