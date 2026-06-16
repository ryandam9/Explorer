package emrtui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newClusterTestModel builds an EMR model wired with the shared table and a
// small inventory, sized to w×h, for the cluster-list tests.
func newClusterTestModel(w, h int) *m {
	mm := &m{
		regions: []string{"us-east-1"},
		filter:  textinput.New(),
		sortCol: -1,
		tbl: table.New(
			table.WithColumns(clusterColumns(false)),
			table.WithFocused(true),
			table.WithStyles(ui.TableStyles()),
			table.WithFrozenColumns(1),
		),
		stepsTbl: newSubTable(stepColumns()),
		yarnTbl:  newSubTable(yarnColumns()),
		hbaseTbl: newSubTable(hbaseColumns()),
		oozieTbl: newSubTable(oozieWFColumns()),
		inv: Inventory{Clusters: []Cluster{
			{Name: "data-platform-production-analytics-cluster-2026", ID: "j-2AB3CD4EF5", State: "WAITING", ReleaseLabel: "emr-7.1.0", Applications: "Spark, HBase, Hive, Hadoop, Tez, Livy", InstanceHours: 128},
			{Name: "etl", ID: "j-9ZY8XW7VU6", State: "TERMINATED_WITH_ERRORS", ReleaseLabel: "emr-6.15.0", Applications: "Spark", InstanceHours: 12, StateReason: "boom"},
			{Name: "mid", ID: "j-5MID000000", State: "RUNNING", ReleaseLabel: "emr-7.0.0", Applications: "Spark", InstanceHours: 64},
		}},
	}
	mm.width, mm.height = w, h
	mm.rebuild()
	return mm
}

// No rendered line may exceed the terminal width (the bug that motivated the
// migration), and the status bar with its shortcuts must always be present.
func TestClusterTableNeverWraps(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 60} {
		mm := newClusterTestModel(w, 24)
		out := mm.View()
		for i, line := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(line); lw > w {
				t.Errorf("width %d: line %d overflows (%d > %d): %q", w, i, lw, w, line)
			}
		}
		if !strings.Contains(out, "rows") || !strings.Contains(out, "quit") {
			t.Errorf("width %d: status hints missing", w)
		}
	}
}

func TestClusterTableNavSelectsCluster(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	if cl, ok := mm.selectedCluster(); !ok || cl.Name != mm.view[0].Name {
		t.Fatalf("initial selection = %+v, ok=%v", cl, ok)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if got := mm.tbl.Cursor(); got != 1 {
		t.Fatalf("cursor after j = %d, want 1", got)
	}
	if cl, _ := mm.selectedCluster(); cl.Name != mm.view[1].Name {
		t.Errorf("selection after j = %q, want %q", cl.Name, mm.view[1].Name)
	}
}

func TestClusterTableSortCycle(t *testing.T) {
	mm := newClusterTestModel(120, 24)

	mm.cycleSort() // → NAME ascending
	if mm.sortCol != colName || !mm.sortAsc {
		t.Fatalf("after first cycle sortCol=%d asc=%v, want NAME asc", mm.sortCol, mm.sortAsc)
	}
	if mm.view[0].Name != "data-platform-production-analytics-cluster-2026" {
		t.Errorf("NAME asc first = %q", mm.view[0].Name)
	}
	if !strings.Contains(mm.tbl.View(), "NAME"+table.SortAscArrow) {
		t.Errorf("header missing NAME sort arrow:\n%s", mm.tbl.View())
	}

	for mm.sortCol != colHRS { // advance to the numeric HRS column
		mm.cycleSort()
	}
	if mm.sortAsc {
		t.Error("HRS should default to descending (most hours first)")
	}
	if mm.view[0].InstanceHours != 128 {
		t.Errorf("HRS desc first = %d hrs, want 128", mm.view[0].InstanceHours)
	}

	// Cycling past the last column returns to the natural order.
	for mm.sortCol != -1 {
		mm.cycleSort()
	}
}

// TestActiveClusterStatesExcludeTerminated guards the default scope: the
// server-side state filter must not list the terminated tail.
func TestActiveClusterStatesExcludeTerminated(t *testing.T) {
	for _, s := range activeClusterStates {
		switch string(s) {
		case "TERMINATED", "TERMINATED_WITH_ERRORS":
			t.Errorf("activeClusterStates must not include terminal state %q", s)
		}
	}
}

// TestTerminatedToggleReloads checks the "t" key flips the scope and kicks off a
// reload (the scope changes what ListClusters returns, so it can't be filtered
// client-side).
func TestTerminatedToggleReloads(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	if mm.showTerminated {
		t.Fatal("dashboard should default to active clusters only")
	}
	cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !mm.showTerminated {
		t.Error("t should toggle showTerminated on")
	}
	if !mm.loading {
		t.Error("t should trigger a reload (loading=true)")
	}
	if len(cmds) == 0 {
		t.Error("t should issue a reload command")
	}
	// The toggle is guarded while a load is in flight; let it complete, then a
	// second t flips the scope back.
	mm.loading = false
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if mm.showTerminated {
		t.Error("second t should toggle showTerminated back off")
	}
}

// TestEMRFindingsPanel checks the findings panel computes deterministic
// findings over the loaded inventory (the terminated-with-errors cluster is the
// loudest signal) and renders without overflowing the terminal.
func TestEMRFindingsPanel(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.openFindings()
	if !mm.findingsActive {
		t.Fatal("openFindings should activate the panel")
	}
	var gotCrit bool
	for _, f := range mm.findingList {
		if f.Severity == findings.SevCritical && f.Resource == "etl" {
			gotCrit = true
		}
	}
	if !gotCrit {
		t.Errorf("expected a CRITICAL finding for the terminated-with-errors cluster, got %+v", mm.findingList)
	}
	out := mm.View()
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("findings line %d overflows (%d > 120): %q", i, lw, line)
		}
	}
	if !strings.Contains(out, "Findings") {
		t.Errorf("findings view missing heading:\n%s", out)
	}
}

// TestEMRStatusBarPinnedToBottom guards issue #237: when the cluster list has
// no data the status bar must stay on the bottom line of the terminal rather
// than floating up behind the short "no results" message.
func TestEMRStatusBarPinnedToBottom(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv = Inventory{} // no clusters
	mm.rebuild()

	out := mm.View()
	if !strings.Contains(out, "No clusters found in scope.") {
		t.Fatalf("expected the empty-list message:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != mm.height {
		t.Errorf("rendered %d lines, want %d (status bar should fill to the bottom)", len(lines), mm.height)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "quit") {
		t.Errorf("status bar is not on the bottom line; last line = %q\nfull:\n%s", last, out)
	}
}

// TestEMREnrichmentGapSuppressesDetailFindings verifies an un-enriched cluster
// (DescribeCluster denied) does not fire detail-derived findings on its blank
// fields — blank means "unknown", not "none".
func TestEMREnrichmentGapSuppressesDetailFindings(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv.Clusters = []Cluster{
		{Name: "ghost", ID: "j-GHOST", Region: "us-east-1", State: "RUNNING", DetailKnown: false},
	}
	mm.inv.EnrichFailures = 1
	mm.openFindings()
	for _, f := range mm.findingList {
		if f.ID == findings.CheckEMRNoLogURI || f.ID == findings.CheckEMRNoSecurityConf {
			t.Errorf("un-enriched cluster should not fire detail-derived finding %s", f.ID)
		}
	}
}

// TestEMREnrichmentGapWarningRenders verifies the enrichment-gap warning shows
// in the list and that reserving its line keeps the status bar on screen
// (layout chrome accounts for it).
func TestEMREnrichmentGapWarningRenders(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv.EnrichFailures = 2
	mm.rebuild()
	out := mm.View()
	if !strings.Contains(out, "could not be enriched") {
		t.Errorf("expected enrichment-gap warning, got:\n%s", out)
	}
	if !strings.Contains(out, "quit") {
		t.Error("status bar missing — the warning line may have clipped it")
	}
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("line %d overflows (%d > 120): %q", i, lw, line)
		}
	}
}

// TestEMRDetailOverlayScrollsAndCloses verifies the cluster-detail overlay is
// scrollable when its content is taller than the viewport, and closes on Esc.
func TestEMRDetailOverlayScrollsAndCloses(t *testing.T) {
	mm := newClusterTestModel(100, 16) // short height → detail taller than the viewport
	cl, _ := mm.selectedCluster()
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !mm.detailActive {
		t.Fatal("d should open the detail overlay")
	}
	out := mm.View() // sizes and fills the viewport
	if !strings.Contains(out, "Cluster — "+cl.Name) {
		t.Errorf("detail overlay missing title:\n%s", out)
	}
	if mm.detailVP.TotalLineCount() <= mm.detailVP.Height {
		t.Fatalf("test needs content taller than the viewport (lines=%d height=%d)",
			mm.detailVP.TotalLineCount(), mm.detailVP.Height)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if mm.detailVP.YOffset == 0 {
		t.Error("j should scroll the detail viewport down")
	}
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 100 {
			t.Errorf("overlay line %d overflows (%d > 100): %q", i, lw, line)
		}
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.detailActive {
		t.Error("Esc should close the detail overlay")
	}
}

// TestEMRRefreshGuardedWhileLoading verifies r (and the t toggle) are no-ops
// while a load is already running, so they can't fire concurrent inventory
// loads.
func TestEMRRefreshGuardedWhileLoading(t *testing.T) {
	mm := newClusterTestModel(120, 24)

	mm.loading = true
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) != 0 {
		t.Errorf("r during a load should not start another (got %d cmds)", len(cmds))
	}
	before := mm.showTerminated
	cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if mm.showTerminated != before {
		t.Error("t during a load should not toggle the terminated scope")
	}
	if len(cmds) != 0 {
		t.Error("t during a load should not start a reload")
	}

	mm.loading = false
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) == 0 || !mm.loading {
		t.Error("r when idle should start a reload")
	}
}

// TestEMRAgeColumnAndSort checks the AGE column renders a compact age and that
// sorting by AGE defaults to oldest-first.
func TestEMRAgeColumnAndSort(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	now := time.Now()
	mm.inv.Clusters = []Cluster{
		{Name: "old", ID: "j-OLD", Region: "us-east-1", State: "RUNNING", Created: now.Add(-72 * time.Hour)},
		{Name: "young", ID: "j-YNG", Region: "us-east-1", State: "RUNNING", Created: now.Add(-1 * time.Hour)},
	}
	mm.rebuild()
	if !strings.Contains(mm.tbl.View(), "3d") {
		t.Errorf("expected AGE 3d for the old cluster:\n%s", mm.tbl.View())
	}

	for mm.sortCol != colAge {
		mm.cycleSort()
	}
	if mm.sortAsc {
		t.Error("AGE should default to descending (oldest first)")
	}
	if mm.view[0].Name != "old" {
		t.Errorf("AGE desc first = %q, want old", mm.view[0].Name)
	}
}

func TestClusterTableFilter(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.filter.SetValue("terminated")
	mm.rebuild()
	if len(mm.view) != 1 || mm.view[0].Name != "etl" {
		t.Errorf("filter 'terminated' => %d rows %+v", len(mm.view), mm.view)
	}
}
