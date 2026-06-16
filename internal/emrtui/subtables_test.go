package emrtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// assertNoWrap checks every rendered line fits the terminal width and the
// status bar with its shortcuts is present.
func assertNoWrap(t *testing.T, mm *m, w int, label string) {
	t.Helper()
	out := mm.View()
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > w {
			t.Errorf("%s width %d: line %d overflows (%d > %d): %q", label, w, i, lw, w, line)
		}
	}
	if !strings.Contains(out, "quit") {
		t.Errorf("%s width %d: status hints missing", label, w)
	}
}

func TestSubViewsRenderNoWrap(t *testing.T) {
	for _, w := range []int{120, 80} {
		mm := newClusterTestModel(w, 24)

		// Steps.
		mm.stepsActive = true
		mm.stepsCluster = mm.view[0]
		mm.setRows(&mm.stepsTbl, 2, func(i int) table.Row {
			steps := []Step{
				{Name: "load-data", State: "COMPLETED", ActionOnFailure: "CONTINUE"},
				{Name: "boom", State: "FAILED", ActionOnFailure: "TERMINATE_CLUSTER", FailureReason: "bootstrap action 1 returned a non-zero return code from a very long message"},
			}
			return stepRow(steps[i])
		})
		mm.steps = []Step{
			{Name: "load-data", State: "COMPLETED", ActionOnFailure: "CONTINUE"},
			{Name: "boom", State: "FAILED", ActionOnFailure: "TERMINATE_CLUSTER", FailureReason: "bootstrap action 1 returned a non-zero return code from a very long message"},
		}
		mm.stepsTbl.SetCursor(1) // select the failed step → footer shows
		assertNoWrap(t, mm, w, "steps")
		mm.stepsActive = false

		// YARN.
		mm.yarnActive = true
		mm.yarnCluster = mm.view[0]
		mm.yarnApps = []YarnApp{{ID: "application_123_0001", State: "RUNNING", FinalStatus: "UNDEFINED", Progress: 42, Queue: "default", User: "hadoop", ElapsedMS: 90000}}
		mm.yarnMetrics = ClusterMetrics{AppsRunning: 1, AllocatedMB: 4096, TotalMB: 16384, AllocatedVCfg: 4, TotalVC: 16}
		mm.setRows(&mm.yarnTbl, len(mm.yarnApps), func(i int) table.Row { return yarnRow(mm.yarnApps[i]) })
		assertNoWrap(t, mm, w, "yarn")
		mm.yarnActive = false

		// HBase.
		mm.hbaseActive = true
		mm.hbaseCluster = mm.view[0]
		mm.hbaseTables = []HBaseTable{{Namespace: "default", Name: "events", State: "ENABLED", Regions: 8, Online: 8, Families: []string{"cf1", "cf2"}}}
		mm.setRows(&mm.hbaseTbl, len(mm.hbaseTables), func(i int) table.Row { return hbaseRow(mm.hbaseTables[i]) })
		assertNoWrap(t, mm, w, "hbase")
		mm.hbaseActive = false

		// Oozie.
		mm.oozieActive = true
		mm.oozieCluster = mm.view[0]
		mm.oozieWF = []OozieWorkflow{{ID: "0000001-wf", AppName: "nightly-orders", Status: "SUCCEEDED", User: "hadoop", StartTime: "Mon, 15 Jun 2026 01:00 GMT"}}
		mm.setOozieRows()
		assertNoWrap(t, mm, w, "oozie")
	}
}

func TestSubViewNavigation(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.hbaseActive = true
	mm.hbaseTables = []HBaseTable{
		{Namespace: "default", Name: "a", State: "ENABLED", Qualified: "default:a"},
		{Namespace: "default", Name: "b", State: "DISABLED", Qualified: "default:b"},
	}
	mm.setRows(&mm.hbaseTbl, len(mm.hbaseTables), func(i int) table.Row { return hbaseRow(mm.hbaseTables[i]) })

	if tbl, ok := mm.selectedHbaseTable(); !ok || tbl.Name != "a" {
		t.Fatalf("initial hbase selection = %+v ok=%v", tbl, ok)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if tbl, _ := mm.selectedHbaseTable(); tbl.Name != "b" {
		t.Errorf("after j, selected = %q want b", tbl.Name)
	}
}
