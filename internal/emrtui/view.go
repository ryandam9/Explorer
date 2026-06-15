package emrtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
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

	if mm.stepsActive {
		sb.WriteString(mm.renderSteps())
	} else if mm.yarnActive {
		sb.WriteString(mm.renderYARN())
	} else {
		sb.WriteString(mm.renderTable())
	}

	sb.WriteString(ui.StatusBar(mm.width, mm.statusLeft(), mm.helpHints()))

	frame := mm.applyToast(sb.String())
	if mm.detailActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Cluster — "+mm.detailCluster.Name, mm.detailBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.appUIActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Application UIs — "+mm.appUICluster.Name, mm.appUIBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.showAbout {
		frame = ui.OverlayCenter(frame, ui.AboutView("About — Amazon EMR", emrAboutText, ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	return frame
}

const emrAboutText = "This is the Amazon EMR dashboard. Each row is a cluster, colour-coded by " +
	"state, showing its release label, the applications installed on it " +
	"(Spark, HBase, Hive, Oozie…) and its size.\n\n" +
	"Press Enter (or s) on a cluster to see its step history: state, duration and " +
	"action-on-failure, with the failure reason inline on a failed step. Press d for " +
	"the cluster detail (release, log URI, role, EC2 attributes).\n\n" +
	"Press L to open the cluster's (or a step's) logs in the S3 browser, and u to open " +
	"a persistent application UI (Spark History, YARN Timeline, Tez) — hosted off-cluster, " +
	"so no SSH tunnel is needed.\n\n" +
	"Press o on a cluster to open it in the AWS console, / to filter, and r to refresh."

// detailBody renders the cluster-detail overlay's contents.
func (mm *m) detailBody() string {
	cl := mm.detailCluster
	var b strings.Builder
	row := func(label, value string) {
		if value == "" {
			value = "—"
		}
		b.WriteString(fmt.Sprintf("%-18s %s\n", label, value))
	}
	row("Cluster ID", cl.ID)
	row("State", stateLabel(cl.State))
	if cl.StateReason != "" {
		row("State reason", cl.StateReason)
	}
	row("Release", cl.ReleaseLabel)
	row("Applications", cl.Applications)
	row("Auto-terminate", boolLabel(cl.AutoTerminate))
	row("Normalized hrs", instanceHours(cl.InstanceHours))
	row("Master DNS", cl.MasterDNS)
	row("Log URI", cl.LogURI)
	row("Service role", cl.ServiceRole)
	row("Security config", cl.SecurityConfig)
	row("Subnet", cl.SubnetID)
	row("Availability zone", cl.AvailabilityAZ)
	row("EC2 key", cl.KeyName)
	return strings.TrimRight(b.String(), "\n")
}

// appUIBody renders the persistent application-UI picker overlay: a short menu
// while idle, or a spinner while the presigned URL is being generated.
func (mm *m) appUIBody() string {
	if mm.appUILoading {
		return mm.spinner.View() + " Generating " + appUIOptions[mm.appUISel].Label + " link…\n\n" +
			"This provisions an off-cluster UI and can take a few seconds."
	}
	var b strings.Builder
	b.WriteString("Open a persistent (off-cluster) application UI — no SSH tunnel needed:\n\n")
	for i, opt := range appUIOptions {
		cursor := "  "
		line := opt.Label
		if i == mm.appUISel {
			cursor = "▸ "
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Render(line)
		}
		b.WriteString(cursor + line + "\n")
	}
	b.WriteString("\n↑/↓ choose · Enter open · Esc cancel")
	return b.String()
}

func boolLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func (mm *m) renderTable() string {
	specs, _ := mm.specsAndRows()
	rows := mm.currentRows()
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(" EMR ▸ Clusters") + "\n")

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
		b.WriteString(fmt.Sprintf("  %s Loading EMR clusters…\n", mm.spinner.View()))
	} else if len(rows) == 0 {
		b.WriteString("  No clusters found in scope.\n")
	} else {
		visible := mm.height - 10
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.sel, len(rows), visible)
		for i := start; i < end; i++ {
			b.WriteString(renderRow(rows[i].cells, widths, i == mm.sel) + "\n")
		}
		// State reason for the selected cluster (terminated-with-errors etc.).
		if mm.sel < len(rows) && rows[mm.sel].cluster != nil && rows[mm.sel].cluster.StateReason != "" {
			b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
				Render("└ "+truncate(rows[mm.sel].cluster.StateReason, contentW-4)) + "\n")
		}
	}

	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

func (mm *m) renderSteps() string {
	specs := []colSpec{{"STARTED", 16}, {"STATE", 14}, {"DURATION", 10}, {"ACTION-ON-FAIL", 18}, {"NAME", 0}}
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" Steps — %s [%s]", mm.stepsCluster.Name, mm.stepsCluster.Region)) + "\n\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(specs, widths)) + "\n")

	switch {
	case mm.stepsLoading:
		b.WriteString(fmt.Sprintf("  %s Loading step history…\n", mm.spinner.View()))
	case mm.stepsErr != nil:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load steps: "+mm.stepsErr.Error()) + "\n")
	case len(mm.steps) == 0:
		b.WriteString("  No steps recorded for this cluster.\n")
	default:
		visible := mm.height - 12
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.stepsSel, len(mm.steps), visible)
		for i := start; i < end; i++ {
			s := mm.steps[i]
			cells := []cell{
				{text: shortTime(s.Created)},
				{text: stateLabel(s.State), color: stateColor(s.State)},
				{text: formatDuration(s.Started, s.Ended)},
				{text: s.ActionOnFailure},
				{text: s.Name},
			}
			b.WriteString(renderRow(cells, widths, i == mm.stepsSel) + "\n")
		}

		// Failure reason for the selected step.
		if mm.stepsSel < len(mm.steps) && mm.steps[mm.stepsSel].FailureReason != "" {
			b.WriteString("\n")
			errText := truncate(mm.steps[mm.stepsSel].FailureReason, contentW-4)
			b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
				Render("✗ "+errText) + "\n")
			if log := mm.steps[mm.stepsSel].FailureLog; log != "" {
				b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
					Render("  log: "+truncate(log, contentW-8)) + "\n")
			}
		}
	}

	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

func (mm *m) renderYARN() string {
	specs := []colSpec{{"APPLICATION", 30}, {"STATE", 12}, {"FINAL", 11}, {"PROG", 6}, {"QUEUE", 12}, {"USER", 10}, {"ELAPSED", 0}}
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" YARN — %s [%s]", mm.yarnCluster.Name, mm.yarnCluster.Region)) + "\n")
	if mm.dialer != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
			Render(fmt.Sprintf("  via %s", mm.dialer.Mode())) + "\n")
	}
	b.WriteString("\n")

	switch {
	case mm.yarnLoading:
		b.WriteString(fmt.Sprintf("  %s Querying the ResourceManager…\n", mm.spinner.View()))
	case mm.yarnErr != nil && emrconn.IsUnreachable(mm.yarnErr):
		// Reachability failure → render the actionable connect helper.
		b.WriteString(emrconn.ConnectHelp(mm.yarnCluster.MasterDNS, yarnPort(mm.dialer)) + "\n")
	case mm.yarnErr != nil:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load YARN apps: "+mm.yarnErr.Error()) + "\n")
	case len(mm.yarnApps) == 0:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
			Render(headerLine(specs, widths)) + "\n")
		b.WriteString("  No applications reported by the ResourceManager.\n")
	default:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
			Render(headerLine(specs, widths)) + "\n")
		visible := mm.height - 13
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.yarnSel, len(mm.yarnApps), visible)
		for i := start; i < end; i++ {
			a := mm.yarnApps[i]
			cells := []cell{
				{text: a.ID},
				{text: stateLabel(a.State), color: stateColor(a.State)},
				{text: a.FinalStatus, color: stateColor(a.FinalStatus)},
				{text: fmt.Sprintf("%.0f%%", a.Progress)},
				{text: a.Queue},
				{text: a.User},
				{text: a.elapsed()},
			}
			b.WriteString(renderRow(cells, widths, i == mm.yarnSel) + "\n")
		}
		// Cluster-metrics footer.
		m := mm.yarnMetrics
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
			Render(fmt.Sprintf("%d running · memory %s/%s · vcores %d/%d",
				m.AppsRunning, mib(m.AllocatedMB), mib(m.TotalMB), m.AllocatedVCfg, m.TotalVC)) + "\n")
	}

	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

// yarnPort returns the YARN daemon port for the connect helper (0 when no
// dialer is configured).
func yarnPort(d *emrconn.Dialer) int {
	if d == nil {
		return emrconn.DefaultYARNPort
	}
	return d.Port(emrconn.ServiceYARN)
}

// mib renders mebibytes as GiB when large enough, else MiB.
func mib(mbytes int64) string {
	if mbytes >= 1024 {
		return fmt.Sprintf("%.1fGB", float64(mbytes)/1024)
	}
	return fmt.Sprintf("%dMB", mbytes)
}

func (mm *m) renderError() string {
	b := "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Bold(true).
		Render("  Amazon EMR dashboard error") + "\n\n"
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
	if mm.stepsActive {
		return fmt.Sprintf("Cluster: %s  ·  Steps: %d", mm.stepsCluster.Name, len(mm.steps))
	}
	if mm.yarnActive {
		return fmt.Sprintf("Cluster: %s  ·  YARN apps: %d", mm.yarnCluster.Name, len(mm.yarnApps))
	}
	regionLabel := mm.regions[0]
	if len(mm.regions) != 1 {
		regionLabel = fmt.Sprintf("all (%d regions)", len(mm.regions))
	}
	return fmt.Sprintf("Region: %s  ·  Clusters: %d", regionLabel, mm.rowCount())
}

func (mm *m) helpHints() []ui.KeyHint {
	if mm.stepsActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "steps"),
			ui.H("L", "logs"),
			ui.H("y", "copy reason"),
			ui.H("Esc", "back"),
			ui.H("i", "about"),
			ui.H("q", "quit"),
		}
	}
	if mm.yarnActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "apps"),
			ui.H("r", "refresh"),
			ui.H("Esc", "back"),
			ui.H("i", "about"),
			ui.H("q", "quit"),
		}
	}
	return []ui.KeyHint{
		ui.H("↑/↓", "rows"),
		ui.H("Enter", "steps"),
		ui.H("d", "detail"),
		ui.H("L", "logs"),
		ui.H("u", "app UIs"),
		ui.H("y", "yarn"),
		ui.H("/", "filter"),
		ui.H("o", "console"),
		ui.H("r", "refresh"),
		ui.H("q", "quit"),
	}
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
// their width, the single flex column (width 0) absorbs the remainder (down to a
// floor), accounting for one space between columns.
func resolveWidths(specs []colSpec, total int) []int {
	widths := make([]int, len(specs))
	gaps := len(specs) - 1
	fixed := gaps
	flex := -1
	for i, s := range specs {
		if s.width == 0 {
			flex = i
			continue
		}
		widths[i] = s.width
		fixed += s.width
	}
	if flex >= 0 {
		w := total - fixed
		if w < 8 {
			w = 8
		}
		widths[flex] = w
	}
	return widths
}

func headerLine(specs []colSpec, widths []int) string {
	parts := make([]string, len(specs))
	for i, s := range specs {
		parts[i] = pad(s.title, widths[i])
	}
	return " " + strings.Join(parts, " ")
}

// renderRow lays cells into fixed-width columns. A selected row is painted with
// the highlight background; otherwise each cell keeps its own colour.
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
