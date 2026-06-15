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
	} else if mm.hbaseActive {
		sb.WriteString(mm.renderHBase())
	} else if mm.oozieActive {
		sb.WriteString(mm.renderOozie())
	} else {
		sb.WriteString(mm.renderTable())
	}

	sb.WriteString("\n" + ui.StatusBar(mm.width, mm.statusLeft(), mm.helpHints()))

	frame := mm.applyToast(sb.String())
	if mm.detailActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Cluster — "+mm.detailCluster.Name, mm.detailBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.appUIActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Application UIs — "+mm.appUICluster.Name, mm.appUIBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.hbaseConfirm && mm.hbaseSel < len(mm.hbaseTables) {
		t := mm.hbaseTables[mm.hbaseSel]
		body := fmt.Sprintf("Count the rows in %q?\n\nThis opens an HBase scanner and reads the whole table "+
			"(up to %s rows). It is read-only but can be slow and load the cluster on a large table.\n\n"+
			"y / Enter — scan now      any other key — cancel", t.Qualified, itoa(scannerCap))
		frame = ui.OverlayCenter(frame, ui.AboutView("Count rows — full scan", body, ui.AboutWidth(mm.width)), mm.width, mm.height)
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
	"Press y for the live YARN application browser, h for the HBase table browser and z for " +
	"the Oozie workflow/coordinator browser; these read on-cluster REST daemons and need " +
	"emr.onCluster configured (off by default).\n\n" +
	"Press S to cycle the column the list is sorted by (R reverses the direction), " +
	"o to open a cluster in the AWS console, / to filter, and r to refresh."

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

// sortedSpecs returns specs with a ↑/↓ marker appended to the title of the
// column the list is currently sorted by, leaving widths untouched.
func (mm *m) sortedSpecs(specs []colSpec) []colSpec {
	if mm.sortCol < 0 || mm.sortCol >= len(specs) {
		return specs
	}
	arrow := " ↑"
	if !mm.sortAsc {
		arrow = " ↓"
	}
	out := append([]colSpec(nil), specs...)
	out[mm.sortCol].title += arrow
	return out
}

// sortLabel describes the active sort for the status bar, e.g. "NAME ↑".
func (mm *m) sortLabel(specs []colSpec) string {
	if mm.sortCol < 0 || mm.sortCol >= len(specs) {
		return ""
	}
	arrow := "↑"
	if !mm.sortAsc {
		arrow = "↓"
	}
	return specs[mm.sortCol].title + " " + arrow
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

	// Header (with a ↑/↓ marker on the sorted column).
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(mm.sortedSpecs(specs), widths)) + "\n")

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
		// The selected cluster's state-change reason (terminated-with-errors etc.)
		// is no longer crammed inline; it lives in the detail overlay (press d),
		// which wraps the full message instead of truncating it.
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

func (mm *m) renderHBase() string {
	specs := []colSpec{{"NAMESPACE", 13}, {"TABLE", 0}, {"STATE", 11}, {"REGIONS", 8}, {"ONLINE", 7}, {"ROWS", 12}, {"FAMILIES", 16}}
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}
	widths := resolveWidths(specs, contentW)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" HBase — %s [%s]", mm.hbaseCluster.Name, mm.hbaseCluster.Region)) + "\n")
	via := ""
	if mm.dialer != nil {
		via = fmt.Sprintf("via %s · ", mm.dialer.Mode())
	}
	note := via + "c counts rows (full scan, asks first)"
	if mm.hbaseCounting {
		note = mm.spinner.View() + " scanning table to count rows…"
	}
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("  "+note) + "\n")
	b.WriteString("\n")

	switch {
	case mm.hbaseLoading:
		b.WriteString(fmt.Sprintf("  %s Querying the HBase REST server…\n", mm.spinner.View()))
	case mm.hbaseErr != nil && emrconn.IsUnreachable(mm.hbaseErr):
		b.WriteString(emrconn.ConnectHelp(mm.hbaseCluster.MasterDNS, hbasePort(mm.dialer)) + "\n")
	case mm.hbaseErr != nil:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load HBase tables: "+mm.hbaseErr.Error()) + "\n")
	case len(mm.hbaseTables) == 0:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
			Render(headerLine(specs, widths)) + "\n")
		b.WriteString("  No tables reported by the HBase REST server.\n")
	default:
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
			Render(headerLine(specs, widths)) + "\n")
		visible := mm.height - 13
		if visible < 3 {
			visible = 3
		}
		start, end := visibleRange(mm.hbaseSel, len(mm.hbaseTables), visible)
		for i := start; i < end; i++ {
			t := mm.hbaseTables[i]
			cells := []cell{
				{text: t.Namespace},
				{text: t.Name},
				{text: t.State, color: hbaseStateColor(t.State)},
				{text: itoa(t.Regions)},
				{text: itoa(t.Online)},
				{text: hbaseRowsLabel(t)},
				{text: strings.Join(t.Families, ",")},
			}
			b.WriteString(renderRow(cells, widths, i == mm.hbaseSel) + "\n")
		}
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).
			Render(fmt.Sprintf("%d tables · region counts are exact; press c to count rows (full table scan)", len(mm.hbaseTables))) + "\n")
	}

	// Row-count confirmation overlay text is composited by View(); here we just
	// render the underlying table.
	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

// hbaseRowsLabel renders a table's row-count cell: "—" until counted, then the
// number (with a "+" when the scan hit the cap).
func hbaseRowsLabel(t HBaseTable) string {
	if !t.Counted {
		return "—"
	}
	if t.CountCapped {
		return itoa(t.RowCount) + "+"
	}
	return itoa(t.RowCount)
}

func (mm *m) renderOozie() string {
	contentW := mm.width - 4
	if contentW < 20 {
		contentW = 20
	}

	var b strings.Builder
	tabLabel := "Workflows"
	if mm.oozieCoords {
		tabLabel = "Coordinators"
	}
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(fmt.Sprintf(" Oozie ▸ %s — %s [%s]", tabLabel, mm.oozieCluster.Name, mm.oozieCluster.Region)) + "\n")
	via := ""
	if mm.dialer != nil {
		via = fmt.Sprintf("via %s · ", mm.dialer.Mode())
	}
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("  "+via+"tab switches Workflows / Coordinators") + "\n\n")

	switch {
	case mm.oozieLoading:
		b.WriteString(fmt.Sprintf("  %s Querying the Oozie server…\n", mm.spinner.View()))
	case mm.oozieErr != nil && emrconn.IsUnreachable(mm.oozieErr):
		b.WriteString(emrconn.ConnectHelp(mm.oozieCluster.MasterDNS, ooziePort(mm.dialer)) + "\n")
	case mm.oozieErr != nil:
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load Oozie jobs: "+mm.oozieErr.Error()) + "\n")
	case mm.oozieCoords:
		b.WriteString(mm.renderOozieCoordinators(contentW))
	default:
		b.WriteString(mm.renderOozieWorkflows(contentW))
	}

	return boxStyle(mm.width, mm.height-4).Render(b.String())
}

func (mm *m) renderOozieWorkflows(contentW int) string {
	specs := []colSpec{{"NAME", 0}, {"STATUS", 14}, {"USER", 12}, {"STARTED", 22}}
	widths := resolveWidths(specs, contentW)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(specs, widths)) + "\n")
	if len(mm.oozieWF) == 0 {
		b.WriteString("  No workflow jobs reported by Oozie.\n")
		return b.String()
	}
	visible := mm.height - 14
	if visible < 3 {
		visible = 3
	}
	start, end := visibleRange(mm.oozieSel, len(mm.oozieWF), visible)
	for i := start; i < end; i++ {
		w := mm.oozieWF[i]
		cells := []cell{
			{text: w.AppName},
			{text: w.Status, color: oozieStateColor(w.Status)},
			{text: w.User},
			{text: w.StartTime},
		}
		b.WriteString(renderRow(cells, widths, i == mm.oozieSel) + "\n")
	}
	return b.String()
}

func (mm *m) renderOozieCoordinators(contentW int) string {
	specs := []colSpec{{"NAME", 0}, {"STATUS", 14}, {"FREQUENCY", 14}, {"NEXT MATERIALIZED", 22}}
	widths := resolveWidths(specs, contentW)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).
		Render(headerLine(specs, widths)) + "\n")
	if len(mm.oozieCoord) == 0 {
		b.WriteString("  No coordinator jobs reported by Oozie.\n")
		return b.String()
	}
	visible := mm.height - 14
	if visible < 3 {
		visible = 3
	}
	start, end := visibleRange(mm.oozieSel, len(mm.oozieCoord), visible)
	for i := start; i < end; i++ {
		c := mm.oozieCoord[i]
		next := c.NextMaterialized
		if next == "" {
			next = "—"
		}
		cells := []cell{
			{text: c.Name},
			{text: c.Status, color: oozieStateColor(c.Status)},
			{text: c.frequency()},
			{text: next},
		}
		b.WriteString(renderRow(cells, widths, i == mm.oozieSel) + "\n")
	}
	return b.String()
}

// oozieStateColor colours an Oozie job status.
func oozieStateColor(status string) string {
	switch strings.ToUpper(status) {
	case "SUCCEEDED":
		return ui.ColorSuccess()
	case "RUNNING", "PREP", "PAUSED", "PREPPAUSED", "RUNNINGWITHERROR":
		return ui.ColorAccent()
	case "FAILED", "KILLED", "SUSPENDED", "DONEWITHERROR", "PREPSUSPENDED", "SUSPENDEDWITHERROR":
		return ui.ColorError()
	default:
		return ui.ColorText()
	}
}

func ooziePort(d *emrconn.Dialer) int {
	if d == nil {
		return emrconn.DefaultOoziePort
	}
	return d.Port(emrconn.ServiceOozie)
}

// hbaseStateColor colours the derived table state.
func hbaseStateColor(state string) string {
	switch state {
	case "ENABLED":
		return ui.ColorSuccess()
	case "DISABLED":
		return ui.ColorMuted()
	case "PARTIAL":
		return ui.ColorError()
	default:
		return ui.ColorText()
	}
}

func hbasePort(d *emrconn.Dialer) int {
	if d == nil {
		return emrconn.DefaultHBasePort
	}
	return d.Port(emrconn.ServiceHBase)
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
	if mm.hbaseActive {
		return fmt.Sprintf("Cluster: %s  ·  HBase tables: %d", mm.hbaseCluster.Name, len(mm.hbaseTables))
	}
	if mm.oozieActive {
		return fmt.Sprintf("Cluster: %s  ·  Workflows: %d · Coordinators: %d", mm.oozieCluster.Name, len(mm.oozieWF), len(mm.oozieCoord))
	}
	regionLabel := mm.regions[0]
	if len(mm.regions) != 1 {
		regionLabel = fmt.Sprintf("all (%d regions)", len(mm.regions))
	}
	left := fmt.Sprintf("Region: %s  ·  Clusters: %d", regionLabel, mm.rowCount())
	if specs, _ := mm.specsAndRows(); mm.sortLabel(specs) != "" {
		left += "  ·  sort: " + mm.sortLabel(specs)
	}
	return left
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
	if mm.hbaseActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "tables"),
			ui.H("c", "count rows"),
			ui.H("r", "refresh"),
			ui.H("Esc", "back"),
			ui.H("q", "quit"),
		}
	}
	if mm.oozieActive {
		return []ui.KeyHint{
			ui.H("↑/↓", "jobs"),
			ui.H("Tab", "wf/coord"),
			ui.H("r", "refresh"),
			ui.H("Esc", "back"),
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
		ui.H("h", "hbase"),
		ui.H("z", "oozie"),
		ui.H("S", "sort"),
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
