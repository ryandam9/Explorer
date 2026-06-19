package lambdatui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/findings"
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

	if mm.codeActive {
		sb.WriteString(mm.renderCode())
	} else if mm.detailActive {
		sb.WriteString(mm.renderDetail())
	} else if mm.findingsActive {
		sb.WriteString(mm.renderFindings())
	} else {
		sb.WriteString(mm.renderTabBar() + "\n")
		sb.WriteString(mm.renderTable())
	}

	// Pin the status bar to the bottom: pad the body with blank lines so the body
	// + the blank separator + the status bar always reach the full terminal
	// height (otherwise a short/empty tab floats the status bar up).
	body := sb.String()
	status := ui.StatusBar(mm.width, mm.statusLeft(), mm.helpHints())
	sep := "\n"
	if mm.height > 0 {
		if n := mm.height - lipgloss.Height(body) - lipgloss.Height(status) + 1; n > 1 {
			sep = strings.Repeat("\n", n)
		}
	}

	frame := mm.applyToast(ui.ClipToSize(body+sep+status, mm.width, mm.height))
	if mm.codeConfirm {
		frame = ui.OverlayCenterBlank(mm.renderCodeConfirm(), mm.width, mm.height)
	}
	if mm.showAbout {
		frame = ui.OverlayCenterBlank(ui.AboutView("About — AWS Lambda", lambdaAboutText, ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	// The debug pane floats over everything (including the detail grid), so its
	// live activity is visible even while the inventory is still loading.
	frame = mm.debug.Overlay(frame, mm.width, mm.height)
	return frame
}

const lambdaAboutText = "This is the AWS Lambda dashboard. Tab across Functions, Layers and Event " +
	"sources; each row shows health at a glance — a function's runtime, memory, " +
	"timeout and state, a layer's latest version and compatible runtimes, an event-" +
	"source mapping's source, state and batch size.\n\n" +
	"Press Enter on a function to open its full configuration as a grid of panels — " +
	"overview, resources & limits, state, VPC networking, environment-variable keys " +
	"(values are never shown), layers, code package, resource policy and tags — each a " +
	"separately scrollable tile (fetched on demand). Tab/arrows move between tiles. On a " +
	"Zip function, v downloads the deployment package (opt-in, after a confirmation) and " +
	"lets you browse and read its source files. Enter on a layer or event source opens " +
	"its panels from the loaded data.\n\n" +
	"Press f for the findings panel — deterministic runtime/health checks (deprecated " +
	"or soon-deprecating runtimes, missing dead-letter queues, failed-state functions) " +
	"over the loaded functions; y copies the suggested fix.\n\n" +
	"On a function, L opens its CloudWatch logs (/aws/lambda/<name>). Press S to cycle " +
	"the column the active tab is sorted by (R reverses the direction), o on any row to " +
	"open it in the AWS console, / to filter, r to refresh, and ~ for the live debug pane."

func (mm *m) renderTabBar() string {
	var parts []string
	for t := tab(0); t < tabCount; t++ {
		label := fmt.Sprintf(" %s (%d) ", tabNames[t], mm.tabCount(t))
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
	bar := "Lambda ▸ " + strings.Join(parts, " ")
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
		b.WriteString(fmt.Sprintf("\n  %s Loading Lambda resources…", mm.spinner.View()))
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

// renderFindings draws the deterministic runtime/health panel over the loaded
// functions. The selected finding's detail and suggested fix sit in the footer.
func (mm *m) renderFindings() string {
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(" Findings — runtime & health checks") + "\n" +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("  deterministic checks over the loaded functions · y copies the fix")

	if len(mm.findingList) == 0 {
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess())).
			Render("✓ No findings for the loaded functions.")
	}

	foot := mm.findingsFooter()
	mm.fitTable(&mm.findingsTbl, lipgloss.Height(head), lipgloss.Height(foot)+1)
	return head + "\n" + ui.TablePanelStyle(true).Render(mm.findingsTbl.View()) +
		"\n" + ui.TableScrollIndicator(&mm.findingsTbl) + "\n" + foot
}

// findingsFooter renders the selected finding's detail and suggested fix.
func (mm *m) findingsFooter() string {
	f, ok := mm.selectedFinding()
	if !ok {
		return ""
	}
	w := mm.width - 6
	out := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Render("  " + truncate(f.Detail, w))
	if f.Fix != "" {
		out += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render("    fix: "+truncate(f.Fix, w-6))
	}
	return out
}

func (mm *m) renderError() string {
	b := "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).
		Render("  AWS Lambda dashboard error") + "\n\n"
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
	if mm.codeActive {
		if mm.codeLoading {
			return mm.codeTitle + "  ·  downloading…"
		}
		if mm.codeViewing {
			return mm.codeTitle + "  ·  " + mm.codeFileName
		}
		return fmt.Sprintf("%s  ·  %d files", mm.codeTitle, len(mm.codeFiles))
	}
	if mm.detailActive {
		if mm.detailLoading {
			return mm.detailTitle + "  ·  loading…"
		}
		return fmt.Sprintf("%s  ·  panel %d/%d", mm.detailTitle, mm.detailFocus+1, len(mm.detailSections))
	}
	if mm.findingsActive {
		return "Findings: " + findings.Summary(mm.findingList)
	}
	regionLabel := mm.regions[0]
	if len(mm.regions) != 1 {
		regionLabel = fmt.Sprintf("all (%d regions)", len(mm.regions))
	}
	return fmt.Sprintf("Region: %s  ·  %s: %d", regionLabel, tabNames[mm.tab], mm.rowCount())
}

func (mm *m) helpHints() []ui.KeyHint {
	if mm.codeActive {
		if mm.codeViewing {
			return []ui.KeyHint{ui.H("↑/↓", "scroll"), ui.H("y", "copy"), ui.H("Esc", "back"), ui.H("q", "quit")}
		}
		return []ui.KeyHint{ui.H("↑/↓", "files"), ui.H("Enter", "open"), ui.H("y", "copy"), ui.H("Esc", "back"), ui.H("q", "quit")}
	}
	if mm.detailActive {
		return []ui.KeyHint{
			ui.H("Tab", "panel"),
			ui.H("↑/↓", "scroll"),
			ui.H("v", "view code"),
			ui.H("Esc", "back"),
			ui.H("i", "about"),
			ui.H("q", "quit"),
		}
	}
	if mm.findingsActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "findings"),
			ui.H("y", "copy fix"),
			ui.H("Esc", "back"),
			ui.H("i", "about"),
			ui.H("q", "quit"),
		}
	}
	hints := []ui.KeyHint{
		ui.H("Tab", "pane"),
		ui.H("↑/↓", "rows"),
		ui.H("Enter", "detail"),
	}
	if mm.tab == tabFunctions {
		hints = append(hints, ui.H("L", "logs"))
	}
	hints = append(hints, ui.H("f", "findings"), ui.H("S", "sort"))
	if mm.sortCol >= 0 {
		hints = append(hints, ui.H("R", "reverse"))
	}
	if hl, hr := mm.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
	}
	hints = append(hints,
		ui.H("/", "filter"),
		ui.H("o", "console"),
		ui.H("r", "refresh"),
		ui.H("~", "debug"),
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
