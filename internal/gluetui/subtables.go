package gluetui

import (
	"fmt"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newGlueTable builds a focused shared-table with the given columns; the first
// column is pinned during horizontal scrolling.
func newGlueTable(cols []table.Column) table.Model {
	return table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithStyles(ui.TableStyles()),
		table.WithFrozenColumns(1),
	)
}

// fitTable sizes a table to fill the space left after its chrome: an optional
// region badge, headerLines above the panel, footerLines below it, the panel
// border (2) and the status bar (1).
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

// --- Runs ------------------------------------------------------------------

func runColumns() []table.Column {
	return []table.Column{
		{Title: "STARTED", Width: 16},
		{Title: "STATE", Width: 12},
		{Title: "DURATION", Width: 10},
		{Title: "DPU-HRS", Width: 8},
		{Title: "EST", Width: 8},
		{Title: "WORKER", Width: 10},
		{Title: "ATTEMPT", Width: 7},
	}
}

func runRow(r JobRun) table.Row {
	return table.Row{
		shortTime(r.Started),
		stateLabel(r.State),
		formatDuration(r.ExecSecs),
		formatDPUHours(r.DPUSeconds),
		formatCost(r.DPUSeconds),
		r.Worker,
		fmt.Sprintf("%d", r.Attempt),
	}
}

func (mm *m) selectedRun() (JobRun, bool) {
	i := mm.runsTbl.Cursor()
	if i < 0 || i >= len(mm.runs) {
		return JobRun{}, false
	}
	return mm.runs[i], true
}
