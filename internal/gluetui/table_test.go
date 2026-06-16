package gluetui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
)

func newGlueTestModel(w, h int) *m {
	mm := &m{
		regions: []string{"us-east-1"},
		filter:  textinput.New(),
		sortCol: -1,
		tbl:     newGlueTable(tabColumns(tabJobs, false)),
		runsTbl: newGlueTable(runColumns()),
		inv: Inventory{
			Jobs: []Job{
				{Name: "nightly-orders-etl-production", Region: "us-east-1", LastRunState: "SUCCEEDED", LastRunSeconds: 842, Worker: "G.2X", GlueVersion: "4.0"},
				{Name: "ingest", Region: "us-east-1", LastRunState: "FAILED", LastRunSeconds: 12, Worker: "G.1X", GlueVersion: "3.0"},
			},
			Crawlers: []Crawler{
				{Name: "catalog-crawler", Region: "us-east-1", State: "READY", LastCrawlStatus: "SUCCEEDED", Database: "analytics", Schedule: "cron(0 1 * * ? *)"},
			},
		},
	}
	mm.width, mm.height = w, h
	mm.rebuild()
	return mm
}

func TestGlueTableNeverWraps(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 60} {
		mm := newGlueTestModel(w, 24)
		// Sweep every tab so each column set is checked.
		for tb := tab(0); tb < tabCount; tb++ {
			mm.tab = tb
			mm.rebuild()
			out := mm.View()
			for i, line := range strings.Split(out, "\n") {
				if lw := ansi.StringWidth(line); lw > w {
					t.Errorf("tab %s width %d: line %d overflows (%d > %d): %q", tabNames[tb], w, i, lw, w, line)
				}
			}
			if !strings.Contains(out, "quit") {
				t.Errorf("tab %s width %d: status hints missing", tabNames[tb], w)
			}
		}
	}
}

func TestGlueTabSwitchRemembersCursor(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.tbl.MoveDown(1) // select second job
	if mm.tbl.Cursor() != 1 {
		t.Fatalf("cursor=%d want 1", mm.tbl.Cursor())
	}
	mm.switchTab(true) // → Crawlers
	if mm.tab != tabCrawlers {
		t.Fatalf("tab=%d want crawlers", mm.tab)
	}
	mm.switchTab(false) // back → Jobs
	if mm.tab != tabJobs {
		t.Fatalf("tab=%d want jobs", mm.tab)
	}
	if mm.tbl.Cursor() != 1 {
		t.Errorf("cursor after round-trip = %d, want remembered 1", mm.tbl.Cursor())
	}
}

func TestGlueSelectedJob(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	if job, ok := mm.selectedJob(); !ok || job.Name != "nightly-orders-etl-production" {
		t.Fatalf("selected job = %+v ok=%v", job, ok)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if job, _ := mm.selectedJob(); job.Name != "ingest" {
		t.Errorf("after j, job = %q want ingest", job.Name)
	}
}

// TestGlueTabCountMatchesRows guards the tab-bar count fast path: tabCount must
// stay equal to len(tabRows) for every tab so the bar can read inventory slice
// lengths instead of rebuilding all six tabs' rows each render frame.
func TestGlueTabCountMatchesRows(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	for tb := tab(0); tb < tabCount; tb++ {
		if got, want := mm.tabCount(tb), len(mm.tabRows(tb)); got != want {
			t.Errorf("tab %s: tabCount=%d, len(tabRows)=%d", tabNames[tb], got, want)
		}
	}
}

// TestGlueFindingsPanel checks the findings panel flags a job whose latest run
// failed, suppresses the checks the inventory can't evaluate (no security-config
// guess), and renders without overflow.
func TestGlueFindingsPanel(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.openFindings()
	if !mm.findingsActive {
		t.Fatal("openFindings should activate the panel")
	}
	var lastRunFailed, sawSecurity bool
	for _, f := range mm.findingList {
		if f.ID == findings.CheckGlueLastRunFailed && f.Resource == "ingest" {
			lastRunFailed = true
		}
		if f.ID == findings.CheckGlueNoSecurityConf {
			sawSecurity = true
		}
	}
	if !lastRunFailed {
		t.Errorf("expected a latest-run-failed finding for ingest, got %+v", mm.findingList)
	}
	if sawSecurity {
		t.Error("GLU-SEC-001 must be suppressed in the TUI panel (security config is unknown at inventory)")
	}
	out := mm.View()
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("findings line %d overflows (%d > 120): %q", i, lw, line)
		}
	}
}

// TestGlueStatusBarPinnedToBottom guards issue #237: on a tab with no data the
// status bar must stay on the bottom line of the terminal rather than floating
// up to the top behind the short "no results" message.
func TestGlueStatusBarPinnedToBottom(t *testing.T) {
	mm := &m{
		regions: []string{"us-east-1"},
		filter:  textinput.New(),
		sortCol: -1,
		tbl:     newGlueTable(tabColumns(tabJobs, false)),
		runsTbl: newGlueTable(runColumns()),
	}
	mm.width, mm.height = 120, 24
	mm.rebuild()

	out := mm.View()
	if !strings.Contains(out, "No jobs found in scope.") {
		t.Fatalf("expected the empty-tab message:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != mm.height {
		t.Errorf("rendered %d lines, want %d (status bar should fill to the bottom)", len(lines), mm.height)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "quit") {
		t.Errorf("status bar is not on the bottom line; last line = %q\nfull:\n%s", last, out)
	}
}

// TestGlueRefreshGuardedWhileLoading verifies r is a no-op while a load is
// already running, so it can't fire concurrent inventory loads.
func TestGlueRefreshGuardedWhileLoading(t *testing.T) {
	mm := newGlueTestModel(120, 24)

	mm.loading = true
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) != 0 {
		t.Errorf("r during a load should not start another (got %d cmds)", len(cmds))
	}

	mm.loading = false
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) == 0 || !mm.loading {
		t.Error("r when idle should start a reload")
	}
}

// TestGlueTabSortByName checks S cycles the sort onto NAME (with a header
// arrow), R reverses it, and switching tabs resets to the natural order.
func TestGlueTabSortByName(t *testing.T) {
	mm := newGlueTestModel(120, 24) // Jobs: nightly-orders-etl-production, ingest

	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")}) // → NAME ascending
	if mm.sortCol != 0 || !mm.sortAsc {
		t.Fatalf("after S: sortCol=%d asc=%v, want col 0 asc", mm.sortCol, mm.sortAsc)
	}
	if mm.view[0].name != "ingest" {
		t.Errorf("NAME asc first = %q, want ingest", mm.view[0].name)
	}
	if !strings.Contains(mm.tbl.View(), "NAME"+table.SortAscArrow) {
		t.Errorf("header missing NAME sort arrow:\n%s", mm.tbl.View())
	}

	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")}) // reverse
	if mm.view[0].name != "nightly-orders-etl-production" {
		t.Errorf("NAME desc first = %q, want nightly", mm.view[0].name)
	}

	mm.switchTab(true) // → Crawlers; sort resets
	if mm.sortCol != -1 {
		t.Errorf("tab switch should reset the sort, got sortCol=%d", mm.sortCol)
	}
}

func TestGlueRunsView(t *testing.T) {
	mm := newGlueTestModel(120, 24)
	mm.runsActive = true
	mm.runsJob = mm.inv.Jobs[1]
	mm.runs = []JobRun{{ID: "jr_1", State: "FAILED", Error: "OOM: container killed", ExecSecs: 30, DPUSeconds: 120, Worker: "G.1X", Attempt: 1}}
	rows := make([]table.Row, 0, len(mm.runs))
	for _, r := range mm.runs {
		rows = append(rows, runRow(r))
	}
	mm.runsTbl.SetRows(rows)
	out := mm.View()
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("runs line %d overflows (%d): %q", i, lw, line)
		}
	}
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "OOM") {
		t.Errorf("runs view missing content:\n%s", out)
	}
}
