package gluetui

import (
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// rowT is one rendered table row plus the source identity needed for actions
// (console links, run drill-down). cells is the shared-table row; the rest
// identifies the underlying resource.
type rowT struct {
	cells  table.Row
	name   string
	region string
	arn    string
	typ    string
	job    *Job
}

// tabColumns returns the shared-table column set for a tab. Widths are floors:
// the table grows each column to fit its content and scrolls horizontally when
// the set overflows. REGION is appended in multi-region scope.
func tabColumns(t tab, multi bool) []table.Column {
	var cols []table.Column
	switch t {
	case tabJobs:
		cols = []table.Column{{Title: "NAME", Width: 10}, {Title: "LAST RUN", Width: 16}, {Title: "STATE", Width: 12}, {Title: "DURATION", Width: 10}, {Title: "WORKER", Width: 10}, {Title: "VERSION", Width: 8}}
	case tabCrawlers:
		cols = []table.Column{{Title: "NAME", Width: 10}, {Title: "STATE", Width: 12}, {Title: "LAST CRAWL", Width: 12}, {Title: "DATABASE", Width: 14}, {Title: "SCHEDULE", Width: 18}}
	case tabTriggers:
		cols = []table.Column{{Title: "NAME", Width: 10}, {Title: "TYPE", Width: 10}, {Title: "STATE", Width: 12}, {Title: "SCHEDULE", Width: 16}, {Title: "WORKFLOW", Width: 14}}
	case tabWorkflows:
		cols = []table.Column{{Title: "NAME", Width: 10}}
	case tabConnections:
		cols = []table.Column{{Title: "NAME", Width: 10}, {Title: "TYPE", Width: 14}, {Title: "STATUS", Width: 12}}
	case tabDatabases:
		cols = []table.Column{{Title: "NAME", Width: 20}, {Title: "DESCRIPTION", Width: 14}}
	}
	if multi {
		cols = append(cols, table.Column{Title: "REGION", Width: 9})
	}
	return cols
}

// tabRows builds the identity+row pairs for a tab. State is shown with its glyph
// (the shared table styles cells uniformly, not per-cell colour).
func (mm *m) tabRows(t tab) []rowT {
	multi := len(mm.regions) > 1
	var rows []rowT
	add := func(r rowT, region string) {
		if multi {
			r.cells = append(r.cells, region)
		}
		rows = append(rows, r)
	}

	switch t {
	case tabJobs:
		for i := range mm.inv.Jobs {
			j := mm.inv.Jobs[i]
			add(rowT{
				cells: table.Row{j.Name, shortTime(j.LastRunStarted), stateLabel(j.LastRunState), formatDuration(j.LastRunSeconds), j.Worker, j.GlueVersion},
				name:  j.Name, region: j.Region, arn: j.ARN, typ: "job", job: &mm.inv.Jobs[i],
			}, j.Region)
		}
	case tabCrawlers:
		for _, cr := range mm.inv.Crawlers {
			add(rowT{
				cells: table.Row{cr.Name, stateLabel(cr.State), stateLabel(cr.LastCrawlStatus), cr.Database, cr.Schedule},
				name:  cr.Name, region: cr.Region, arn: cr.ARN, typ: "crawler",
			}, cr.Region)
		}
	case tabTriggers:
		for _, tr := range mm.inv.Triggers {
			add(rowT{
				cells: table.Row{tr.Name, tr.Type, stateLabel(tr.State), tr.Schedule, tr.Workflow},
				name:  tr.Name, region: tr.Region, arn: tr.ARN, typ: "trigger",
			}, tr.Region)
		}
	case tabWorkflows:
		for _, wf := range mm.inv.Workflows {
			add(rowT{
				cells: table.Row{wf.Name},
				name:  wf.Name, region: wf.Region, arn: wf.ARN, typ: "workflow",
			}, wf.Region)
		}
	case tabConnections:
		for _, conn := range mm.inv.Connections {
			add(rowT{
				cells: table.Row{conn.Name, conn.Type, stateLabel(conn.Status)},
				name:  conn.Name, region: conn.Region, arn: conn.ARN, typ: "connection",
			}, conn.Region)
		}
	case tabDatabases:
		for _, db := range mm.inv.Databases {
			add(rowT{
				cells: table.Row{db.Name, db.Description},
				name:  db.Name, region: db.Region, arn: db.ARN, typ: "database",
			}, db.Region)
		}
	}
	return rows
}

// tabCount returns how many resources a tab holds. It mirrors len(tabRows(t))
// (tabRows emits one row per resource and never filters) but reads the
// inventory slice length directly, so the tab bar can show per-tab counts every
// render frame without rebuilding all six tabs' rows.
func (mm *m) tabCount(t tab) int {
	switch t {
	case tabJobs:
		return len(mm.inv.Jobs)
	case tabCrawlers:
		return len(mm.inv.Crawlers)
	case tabTriggers:
		return len(mm.inv.Triggers)
	case tabWorkflows:
		return len(mm.inv.Workflows)
	case tabConnections:
		return len(mm.inv.Connections)
	case tabDatabases:
		return len(mm.inv.Databases)
	}
	return 0
}

// buildView returns the active tab's rows filtered by the active filter term.
func (mm *m) buildView() []rowT {
	rows := mm.tabRows(mm.tab)
	term := strings.ToLower(strings.TrimSpace(mm.filter.Value()))
	if term == "" {
		return rows
	}
	out := make([]rowT, 0, len(rows))
	for _, r := range rows {
		if rowMatches(r, term) {
			out = append(out, r)
		}
	}
	return out
}

// rowMatches reports whether any cell (or the region) contains term.
func rowMatches(r rowT, term string) bool {
	if strings.Contains(strings.ToLower(r.region), term) {
		return true
	}
	for _, c := range r.cells {
		if strings.Contains(strings.ToLower(c), term) {
			return true
		}
	}
	return false
}

func (mm *m) rowCount() int { return len(mm.view) }

// selectedRow returns the highlighted row of the active tab.
func (mm *m) selectedRow() (rowT, bool) {
	i := mm.tbl.Cursor()
	if i < 0 || i >= len(mm.view) {
		return rowT{}, false
	}
	return mm.view[i], true
}

// selectedJob returns the highlighted job on the Jobs tab.
func (mm *m) selectedJob() (Job, bool) {
	if mm.tab != tabJobs {
		return Job{}, false
	}
	r, ok := mm.selectedRow()
	if !ok || r.job == nil {
		return Job{}, false
	}
	return *r.job, true
}

// selectedResource returns the highlighted row as a model.Resource for console
// linking.
func (mm *m) selectedResource() (model.Resource, bool) {
	r, ok := mm.selectedRow()
	if !ok {
		return model.Resource{}, false
	}
	return model.Resource{
		Service: "glue", Type: r.typ, Region: r.region, ID: r.name, ARN: r.arn,
	}, true
}
