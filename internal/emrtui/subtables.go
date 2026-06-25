package emrtui

import (
	"fmt"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newSubTable builds a focused shared-table for a drill-down sub-view (steps,
// YARN, HBase, Oozie). Rows are filled in when their data loads; the first
// column is pinned during horizontal scrolling.
func newSubTable(cols []table.Column) table.Model {
	return table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)
}

// fitTable sizes a sub-view table to fill the space left after its chrome: an
// optional region badge, headerLines above the panel, footerLines below it, the
// panel border (2) and the status bar (1). Called from each render so a view's
// exact chrome is accounted for.
func (mm *m) fitTable(tbl *table.Model, headerLines, footerLines int) {
	if mm.width <= 0 || mm.height <= 0 {
		return
	}
	tbl.SetWidth(mm.width - 4)
	badge := 0
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		badge = 1
	}
	h := mm.height - badge - headerLines - footerLines - 3
	if h < 3 {
		h = 3
	}
	tbl.SetHeight(h)
}

// setRows fills a sub-view table with n rows built by f, resetting the cursor
// to the top (used when a sub-view's data first loads).
func (mm *m) setRows(tbl *table.Model, n int, f func(i int) table.Row) {
	rows := make([]table.Row, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, f(i))
	}
	tbl.SetRows(rows)
	tbl.SetCursor(0)
}

// --- Steps -----------------------------------------------------------------

func stepColumns() []table.Column {
	return []table.Column{
		{Title: "STARTED", Width: 16},
		{Title: "STATE", Width: 12},
		{Title: "DURATION", Width: 10},
		{Title: "ACTION-ON-FAIL", Width: 16},
		{Title: "NAME", Width: 8},
	}
}

func stepRow(s Step) table.Row {
	return table.Row{
		shortTime(s.Created),
		stateLabel(s.State),
		formatDuration(s.Started, s.Ended),
		s.ActionOnFailure,
		truncate(s.Name, 40),
	}
}

func (mm *m) selectedStep() (Step, bool) {
	i := mm.stepsTbl.Cursor()
	if i < 0 || i >= len(mm.steps) {
		return Step{}, false
	}
	return mm.steps[i], true
}

// --- YARN ------------------------------------------------------------------

func yarnColumns() []table.Column {
	return []table.Column{
		{Title: "APPLICATION", Width: 22},
		{Title: "STATE", Width: 10},
		{Title: "FINAL", Width: 10},
		{Title: "PROG", Width: 5},
		{Title: "QUEUE", Width: 10},
		{Title: "USER", Width: 8},
		{Title: "ELAPSED", Width: 8},
	}
}

func yarnRow(a YarnApp) table.Row {
	return table.Row{
		a.ID,
		stateLabel(a.State),
		a.FinalStatus,
		fmt.Sprintf("%.0f%%", a.Progress),
		a.Queue,
		a.User,
		a.elapsed(),
	}
}

// --- HBase -----------------------------------------------------------------

func hbaseColumns() []table.Column {
	return []table.Column{
		{Title: "NAMESPACE", Width: 12},
		{Title: "TABLE", Width: 10},
		{Title: "STATE", Width: 10},
		{Title: "REGIONS", Width: 7},
		{Title: "ONLINE", Width: 6},
		{Title: "ROWS", Width: 8},
		{Title: "FAMILIES", Width: 12},
	}
}

func (mm *m) selectedHbaseTable() (HBaseTable, bool) {
	i := mm.hbaseTbl.Cursor()
	if i < 0 || i >= len(mm.hbaseTables) {
		return HBaseTable{}, false
	}
	return mm.hbaseTables[i], true
}

func hbaseRow(t HBaseTable) table.Row {
	return table.Row{
		t.Namespace,
		t.Name,
		t.State,
		itoa(t.Regions),
		itoa(t.Online),
		hbaseRowsLabel(t),
		strings.Join(t.Families, ","),
	}
}

// --- HDFS DataNodes ---------------------------------------------------------

func hdfsColumns() []table.Column {
	return []table.Column{
		{Title: "DATANODE", Width: 28},
		{Title: "STATE", Width: 14},
		{Title: "USED", Width: 10},
		{Title: "CAPACITY", Width: 10},
		{Title: "USED%", Width: 6},
		{Title: "BLOCKS", Width: 9},
		{Title: "CONTACT", Width: 8},
	}
}

func hdfsRow(dn DataNode) table.Row {
	return table.Row{
		dn.Name,
		dn.State,
		humanBytes(dn.Used),
		humanBytes(dn.Capacity),
		dnUsedPct(dn),
		itoa64(dn.NumBlocks),
		itoa64(dn.LastContact) + "s",
	}
}

// --- Oozie -----------------------------------------------------------------

func oozieWFColumns() []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 12},
		{Title: "STATUS", Width: 14},
		{Title: "USER", Width: 10},
		{Title: "STARTED", Width: 20},
	}
}

func oozieCoordColumns() []table.Column {
	return []table.Column{
		{Title: "NAME", Width: 12},
		{Title: "STATUS", Width: 14},
		{Title: "FREQUENCY", Width: 14},
		{Title: "NEXT MATERIALIZED", Width: 20},
	}
}

func oozieWFRow(w OozieWorkflow) table.Row {
	return table.Row{w.AppName, w.Status, w.User, w.StartTime}
}

func oozieCoordRow(c OozieCoordinator) table.Row {
	next := c.NextMaterialized
	if next == "" {
		next = "—"
	}
	return table.Row{c.Name, c.Status, c.frequency(), next}
}

// setOozieRows repopulates the Oozie table for the active tab (workflows or
// coordinators), swapping the column set and rows together.
func (mm *m) setOozieRows() {
	if mm.oozieCoords {
		mm.oozieTbl.SetColumns(oozieCoordColumns())
		rows := make([]table.Row, 0, len(mm.oozieCoord))
		for _, c := range mm.oozieCoord {
			rows = append(rows, oozieCoordRow(c))
		}
		mm.oozieTbl.SetRows(rows)
		return
	}
	mm.oozieTbl.SetColumns(oozieWFColumns())
	rows := make([]table.Row, 0, len(mm.oozieWF))
	for _, w := range mm.oozieWF {
		rows = append(rows, oozieWFRow(w))
	}
	mm.oozieTbl.SetRows(rows)
}
