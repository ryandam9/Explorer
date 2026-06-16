package emrtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
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
	} else if mm.findingsActive {
		sb.WriteString(mm.renderFindings())
	} else {
		sb.WriteString(mm.renderTable())
	}

	// Pin the status bar to the bottom of the terminal. When the list has data
	// the table is fit to fill the height, but on an empty/loading view the body
	// is short and the status bar would otherwise float up to the top (issue
	// #237). Pad the body with blank lines so the body + the blank separator +
	// the status bar always reach the full terminal height.
	body := sb.String()
	status := ui.StatusBar(mm.width, mm.statusLeft(), mm.helpHints())
	sep := "\n"
	if mm.height > 0 {
		// n newlines between the body and status bar so the two together exactly
		// fill the terminal height (lipgloss.Height = newline count + 1).
		if n := mm.height - lipgloss.Height(body) - lipgloss.Height(status) + 1; n > 1 {
			sep = strings.Repeat("\n", n)
		}
	}

	frame := mm.applyToast(ui.ClipToSize(body+sep+status, mm.width, mm.height))
	if mm.detailActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Cluster — "+mm.detailCluster.Name, mm.detailBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if mm.appUIActive {
		frame = ui.OverlayCenter(frame, ui.AboutView("Application UIs — "+mm.appUICluster.Name, mm.appUIBody(), ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	if t, ok := mm.selectedHbaseTable(); mm.hbaseConfirm && ok {
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
	"the cluster detail (release, log URI, role, EC2 attributes), and f for the " +
	"findings panel — deterministic posture/cost checks (idle/long-running clusters, " +
	"no log destination or security configuration, terminated-with-errors) over the " +
	"loaded clusters.\n\n" +
	"Press L to open the cluster's (or a step's) logs in the S3 browser, and u to open " +
	"a persistent application UI (Spark History, YARN Timeline, Tez) — hosted off-cluster, " +
	"so no SSH tunnel is needed.\n\n" +
	"Press y for the live YARN application browser, h for the HBase table browser and z for " +
	"the Oozie workflow/coordinator browser; these read on-cluster REST daemons and need " +
	"emr.onCluster configured (off by default).\n\n" +
	"The list shows only live clusters by default; press t to include the terminated " +
	"tail (and again to hide it).\n\n" +
	"Press S to cycle the column the list is sorted by (R reverses the direction), " +
	"o to open a cluster in the AWS console, / to filter, and r to refresh."

// detailBody renders the cluster-detail overlay's contents.
func (mm *m) detailBody() string {
	cl := mm.detailCluster
	var b strings.Builder
	if !cl.DetailKnown {
		// Enrichment failed for this cluster: the detail fields were never
		// populated, so make clear they are unknown rather than empty.
		b.WriteString(errLine("⚠ Detail unavailable — DescribeCluster was denied or throttled.") + "\n")
		b.WriteString(muted("  The fields below the basics are unknown, not necessarily unset.") + "\n\n")
	}
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

// pluralizeClusters renders "1 cluster" / "N clusters" for the enrichment-gap
// warning.
func pluralizeClusters(n int) string {
	if n == 1 {
		return "1 cluster could not be enriched"
	}
	return fmt.Sprintf("%d clusters could not be enriched", n)
}

func (mm *m) renderTable() string {
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

	// Enrichment gap: some clusters were listed but DescribeCluster was
	// denied/throttled, so their release/apps/log/security columns are unknown
	// (blank ≠ "none"). Flag it in plain language rather than letting the gaps
	// look like real values.
	if n := mm.inv.EnrichFailures; n > 0 {
		b.WriteString(errLine(fmt.Sprintf("  ⚠ %s — DescribeCluster denied/throttled; some columns are unknown (press d for detail).",
			pluralizeClusters(n))) + "\n")
	}

	switch {
	case mm.loading:
		b.WriteString(fmt.Sprintf("\n  %s Loading EMR clusters…", mm.spinner.View()))
	case len(mm.view) == 0:
		b.WriteString("\n  No clusters found in scope.")
	default:
		// The shared table auto-fits columns, scrolls wide column sets, and draws
		// its own vertical scrollbar; the panel and the "more columns" hint mirror
		// the other table dashboards.
		b.WriteString(ui.TablePanelStyle(true).Render(mm.tbl.View()))
		if hint := ui.TableScrollIndicator(&mm.tbl); hint != "" {
			b.WriteString("\n" + hint)
		}
	}

	// The selected cluster's state-change reason (terminated-with-errors etc.)
	// lives in the detail overlay (press d), which wraps the full message.
	return b.String()
}

// heading / muted / errLine are the shared inline text styles for the sub-view
// chrome (kept here so the drill-downs read uniformly).
func heading(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(s)
}
func muted(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(s)
}
func errLine(s string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).Render(s)
}

// renderSubTable composes a drill-down sub-view: the heading block, the shared
// table panel sized to fill the remaining space, a column-scroll hint and an
// optional footer line. A hint line is always reserved so the table height is
// stable whether or not columns are scrolled.
func (mm *m) renderSubTable(tbl *table.Model, head, foot string) string {
	footLines := 0
	if foot != "" {
		footLines = lipgloss.Height(foot)
	}
	mm.fitTable(tbl, lipgloss.Height(head), 1+footLines)
	out := head + "\n" + ui.TablePanelStyle(true).Render(tbl.View()) + "\n" + ui.TableScrollIndicator(tbl)
	if foot != "" {
		out += "\n" + foot
	}
	return out
}

func (mm *m) renderSteps() string {
	head := heading(fmt.Sprintf(" Steps — %s [%s]", mm.stepsCluster.Name, mm.stepsCluster.Region))
	switch {
	case mm.stepsLoading:
		return head + fmt.Sprintf("\n\n  %s Loading step history…", mm.spinner.View())
	case mm.stepsErr != nil:
		return head + "\n\n  " + errLine("Could not load steps: "+mm.stepsErr.Error())
	case len(mm.steps) == 0:
		return head + "\n\n  No steps recorded for this cluster."
	default:
		return mm.renderSubTable(&mm.stepsTbl, head, mm.stepsFooter())
	}
}

// stepsFooter renders the selected step's failure reason (and log path) below
// the table; empty when the step succeeded.
func (mm *m) stepsFooter() string {
	s, ok := mm.selectedStep()
	if !ok || s.FailureReason == "" {
		return ""
	}
	w := mm.width - 6
	out := errLine("  ✗ " + truncate(s.FailureReason, w))
	if s.FailureLog != "" {
		out += "\n" + muted("    log: "+truncate(s.FailureLog, w-2))
	}
	return out
}

func (mm *m) renderYARN() string {
	head := heading(fmt.Sprintf(" YARN — %s [%s]", mm.yarnCluster.Name, mm.yarnCluster.Region))
	if mm.dialer != nil {
		head += "\n" + muted(fmt.Sprintf("  via %s", mm.dialer.Mode()))
	}
	switch {
	case mm.yarnLoading:
		return head + fmt.Sprintf("\n\n  %s Querying the ResourceManager…", mm.spinner.View())
	case mm.yarnErr != nil && emrconn.IsUnreachable(mm.yarnErr):
		// Reachability failure → render the actionable connect helper.
		return head + "\n\n" + emrconn.ConnectHelp(mm.yarnCluster.MasterDNS, yarnPort(mm.dialer))
	case mm.yarnErr != nil:
		return head + "\n\n  " + errLine("Could not load YARN apps: "+mm.yarnErr.Error())
	case len(mm.yarnApps) == 0:
		return head + "\n\n  No applications reported by the ResourceManager."
	default:
		mtr := mm.yarnMetrics
		foot := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Render(
			fmt.Sprintf("  %d running · memory %s/%s · vcores %d/%d",
				mtr.AppsRunning, mib(mtr.AllocatedMB), mib(mtr.TotalMB), mtr.AllocatedVCfg, mtr.TotalVC))
		return mm.renderSubTable(&mm.yarnTbl, head, foot)
	}
}

func (mm *m) renderHBase() string {
	head := heading(fmt.Sprintf(" HBase — %s [%s]", mm.hbaseCluster.Name, mm.hbaseCluster.Region))
	via := ""
	if mm.dialer != nil {
		via = fmt.Sprintf("via %s · ", mm.dialer.Mode())
	}
	note := via + "c counts rows (full scan, asks first)"
	if mm.hbaseCounting {
		note = mm.spinner.View() + " scanning table to count rows…"
	}
	head += "\n" + muted("  "+note)

	switch {
	case mm.hbaseLoading:
		return head + fmt.Sprintf("\n\n  %s Querying the HBase REST server…", mm.spinner.View())
	case mm.hbaseErr != nil && emrconn.IsUnreachable(mm.hbaseErr):
		return head + "\n\n" + emrconn.ConnectHelp(mm.hbaseCluster.MasterDNS, hbasePort(mm.dialer))
	case mm.hbaseErr != nil:
		return head + "\n\n  " + errLine("Could not load HBase tables: "+mm.hbaseErr.Error())
	case len(mm.hbaseTables) == 0:
		return head + "\n\n  No tables reported by the HBase REST server."
	default:
		foot := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Render(
			fmt.Sprintf("  %d tables · region counts are exact; press c to count rows (full table scan)", len(mm.hbaseTables)))
		return mm.renderSubTable(&mm.hbaseTbl, head, foot)
	}
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
	tabLabel := "Workflows"
	if mm.oozieCoords {
		tabLabel = "Coordinators"
	}
	head := heading(fmt.Sprintf(" Oozie ▸ %s — %s [%s]", tabLabel, mm.oozieCluster.Name, mm.oozieCluster.Region))
	via := ""
	if mm.dialer != nil {
		via = fmt.Sprintf("via %s · ", mm.dialer.Mode())
	}
	head += "\n" + muted("  "+via+"tab switches Workflows / Coordinators")

	switch {
	case mm.oozieLoading:
		return head + fmt.Sprintf("\n\n  %s Querying the Oozie server…", mm.spinner.View())
	case mm.oozieErr != nil && emrconn.IsUnreachable(mm.oozieErr):
		return head + "\n\n" + emrconn.ConnectHelp(mm.oozieCluster.MasterDNS, ooziePort(mm.dialer))
	case mm.oozieErr != nil:
		return head + "\n\n  " + errLine("Could not load Oozie jobs: "+mm.oozieErr.Error())
	case mm.oozieRowCount() == 0:
		what := "workflow"
		if mm.oozieCoords {
			what = "coordinator"
		}
		return head + "\n\n  No " + what + " jobs reported by Oozie."
	default:
		return mm.renderSubTable(&mm.oozieTbl, head, "")
	}
}

// renderFindings draws the deterministic posture/cost panel over the loaded
// inventory. The selected finding's detail and suggested fix sit in the footer.
func (mm *m) renderFindings() string {
	head := heading(" Findings — posture & cost checks") + "\n" +
		muted("  deterministic checks over the loaded clusters · y copies the fix")
	if len(mm.findingList) == 0 {
		return head + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess())).
			Render("✓ No findings for the loaded clusters.")
	}
	return mm.renderSubTable(&mm.findingsTbl, head, mm.findingsFooter())
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
		out += "\n" + muted("    fix: "+truncate(f.Fix, w-6))
	}
	return out
}

func ooziePort(d *emrconn.Dialer) int {
	if d == nil {
		return emrconn.DefaultOoziePort
	}
	return d.Port(emrconn.ServiceOozie)
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
	if mm.findingsActive {
		return "Findings: " + findings.Summary(mm.findingList)
	}
	regionLabel := mm.regions[0]
	if len(mm.regions) != 1 {
		regionLabel = fmt.Sprintf("all (%d regions)", len(mm.regions))
	}
	// The active sort is shown by the ↑/↓ arrow on the column header.
	scope := "active"
	if mm.showTerminated {
		scope = "all states"
	}
	return fmt.Sprintf("Region: %s  ·  Clusters: %d (%s)", regionLabel, mm.rowCount(), scope)
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
		ui.H("↑/↓", "rows"),
		ui.H("Enter", "steps"),
		ui.H("d", "detail"),
		ui.H("f", "findings"),
		ui.H("L", "logs"),
		ui.H("u", "app UIs"),
		ui.H("y", "yarn"),
		ui.H("h", "hbase"),
		ui.H("z", "oozie"),
		ui.H("S", "sort"),
	}
	if mm.sortCol >= 0 {
		hints = append(hints, ui.H("R", "reverse"))
	}
	termLabel := "show terminated"
	if mm.showTerminated {
		termLabel = "hide terminated"
	}
	hints = append(hints, ui.H("t", termLabel))
	if hl, hr := mm.tbl.ColScrollInfo(); hl+hr > 0 {
		hints = append(hints, ui.H("</>", "columns"))
	}
	return append(hints,
		ui.H("/", "filter"),
		ui.H("o", "console"),
		ui.H("r", "refresh"),
		ui.H("q", "quit"),
	)
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
