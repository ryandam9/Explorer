package gluetui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

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
		label := fmt.Sprintf(" %s (%d) ", tabNames[t], len(mm.tabRows(t)))
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
	bar := "Glue ▸ " + strings.Join(parts, " ")
	// Clip to the terminal width so a wide tab set never wraps onto a second line.
	if mm.width > 0 {
		bar = ansi.Truncate(bar, mm.width, "…")
	}
	return bar
}

func (mm *m) renderTable() string {
	var b strings.Builder

	// Filter line.
	if mm.filterActive {
		b.WriteString(" " + mm.filter.View() + "\n")
	} else if v := mm.filter.Value(); v != "" {
		b.WriteString("  filter: " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(v) + "  (/ to edit)\n")
	} else {
		b.WriteString("  (/ to filter)\n")
	}

	switch {
	case mm.loading:
		b.WriteString(fmt.Sprintf("\n  %s Loading Glue resources…", mm.spinner.View()))
	case len(mm.view) == 0:
		b.WriteString("\n  No " + strings.ToLower(tabNames[mm.tab]) + " found in scope.")
	default:
		// fitTable accounts for the tab bar (1) and filter line (1) above, and the
		// column-scroll hint (1) below.
		mm.fitTable(&mm.tbl, 2, 1)
		b.WriteString(ui.TablePanelStyle(true).Render(mm.tbl.View()))
		b.WriteString("\n" + ui.TableScrollIndicator(&mm.tbl))
	}
	return b.String()
}

func (mm *m) renderRuns() string {
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" Runs — %s [%s]", mm.runsJob.Name, mm.runsJob.Region))

	switch {
	case mm.runsLoading:
		return head + fmt.Sprintf("\n\n  %s Loading run history…", mm.spinner.View())
	case mm.runsErr != nil:
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load runs: "+mm.runsErr.Error())
	case len(mm.runs) == 0:
		return head + "\n\n  No runs recorded for this job."
	default:
		var foot strings.Builder
		if r, ok := mm.selectedRun(); ok && r.Error != "" {
			foot.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
				Render("  ✗ "+truncate(r.Error, mm.width-6)) + "\n")
		}
		dpu, cost := runsTotals(mm.runs)
		foot.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
			Render(fmt.Sprintf("  %d runs · %.2f DPU-hrs ≈ $%.2f (estimate)", len(mm.runs), dpu, cost)))

		footStr := foot.String()
		mm.fitTable(&mm.runsTbl, 1, lipgloss.Height(footStr)+1)
		return head + "\n" + ui.TablePanelStyle(true).Render(mm.runsTbl.View()) +
			"\n" + ui.TableScrollIndicator(&mm.runsTbl) + "\n" + footStr
	}
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
	if hl, hr := mm.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
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
