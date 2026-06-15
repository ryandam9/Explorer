package gluetui

import (
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// cell is one table cell: its text and an optional theme colour role ("" =
// default text colour).
type cell struct {
	text  string
	color string
}

// colSpec describes a column: its header and fixed width (0 = flexible, takes
// the remaining width; at most one flex column per tab).
type colSpec struct {
	title string
	width int
}

// rowT is a rendered table row plus the source identity needed for actions
// (console links, run drill-down).
type rowT struct {
	cells  []cell
	name   string
	region string
	arn    string
	typ    string
	job    *Job
}

// resourceType maps the active tab to the model.Resource type string used for
// console links.
func (t tab) resourceType() string {
	switch t {
	case tabJobs:
		return "job"
	case tabCrawlers:
		return "crawler"
	case tabTriggers:
		return "trigger"
	case tabWorkflows:
		return "workflow"
	case tabConnections:
		return "connection"
	default:
		return "database"
	}
}

// specsAndRows returns the column layout and the unfiltered rows for a tab.
func (mm *m) specsAndRows(t tab) ([]colSpec, []rowT) {
	multi := len(mm.regions) > 1
	var specs []colSpec
	var rows []rowT

	switch t {
	case tabJobs:
		specs = []colSpec{{"NAME", 0}, {"LAST RUN", 16}, {"STATE", 14}, {"DURATION", 10}, {"WORKER", 12}, {"VERSION", 8}}
		for i := range mm.inv.Jobs {
			j := mm.inv.Jobs[i]
			rows = append(rows, rowT{
				cells: []cell{
					{text: j.Name}, {text: shortTime(j.LastRunStarted)},
					{text: stateLabel(j.LastRunState), color: stateColor(j.LastRunState)},
					{text: formatDuration(j.LastRunSeconds)}, {text: j.Worker}, {text: j.GlueVersion},
				},
				name: j.Name, region: j.Region, arn: j.ARN, typ: "job", job: &mm.inv.Jobs[i],
			})
		}
	case tabCrawlers:
		specs = []colSpec{{"NAME", 0}, {"STATE", 12}, {"LAST CRAWL", 12}, {"DATABASE", 18}, {"SCHEDULE", 22}}
		for _, cr := range mm.inv.Crawlers {
			rows = append(rows, rowT{
				cells: []cell{
					{text: cr.Name},
					{text: stateLabel(cr.State), color: stateColor(cr.State)},
					{text: stateLabel(cr.LastCrawlStatus), color: stateColor(cr.LastCrawlStatus)},
					{text: cr.Database}, {text: cr.Schedule},
				},
				name: cr.Name, region: cr.Region, arn: cr.ARN, typ: "crawler",
			})
		}
	case tabTriggers:
		specs = []colSpec{{"NAME", 0}, {"TYPE", 12}, {"STATE", 12}, {"SCHEDULE", 20}, {"WORKFLOW", 16}}
		for _, tr := range mm.inv.Triggers {
			rows = append(rows, rowT{
				cells: []cell{
					{text: tr.Name}, {text: tr.Type},
					{text: stateLabel(tr.State), color: stateColor(tr.State)},
					{text: tr.Schedule}, {text: tr.Workflow},
				},
				name: tr.Name, region: tr.Region, arn: tr.ARN, typ: "trigger",
			})
		}
	case tabWorkflows:
		specs = []colSpec{{"NAME", 0}}
		for _, wf := range mm.inv.Workflows {
			rows = append(rows, rowT{
				cells: []cell{{text: wf.Name}},
				name:  wf.Name, region: wf.Region, arn: wf.ARN, typ: "workflow",
			})
		}
	case tabConnections:
		specs = []colSpec{{"NAME", 0}, {"TYPE", 16}, {"STATUS", 12}}
		for _, conn := range mm.inv.Connections {
			rows = append(rows, rowT{
				cells: []cell{
					{text: conn.Name}, {text: conn.Type},
					{text: stateLabel(conn.Status), color: stateColor(conn.Status)},
				},
				name: conn.Name, region: conn.Region, arn: conn.ARN, typ: "connection",
			})
		}
	case tabDatabases:
		specs = []colSpec{{"NAME", 24}, {"DESCRIPTION", 0}}
		for _, db := range mm.inv.Databases {
			rows = append(rows, rowT{
				cells: []cell{{text: db.Name}, {text: db.Description}},
				name:  db.Name, region: db.Region, arn: db.ARN, typ: "database",
			})
		}
	}

	if multi {
		specs = append(specs, colSpec{"REGION", 14})
		for i := range rows {
			rows[i].cells = append(rows[i].cells, cell{text: rows[i].region})
		}
	}
	return specs, rows
}

// currentRows returns the filtered rows for the active tab.
func (mm *m) currentRows() []rowT {
	_, rows := mm.specsAndRows(mm.tab)
	term := strings.ToLower(strings.TrimSpace(mm.filter.Value()))
	if term == "" {
		return rows
	}
	var out []rowT
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
		if strings.Contains(strings.ToLower(c.text), term) {
			return true
		}
	}
	return false
}

func (mm *m) rowCount() int { return len(mm.currentRows()) }

// rowsForTab returns the unfiltered rows for a tab (used for clamping).
func (mm *m) rowsForTab(t tab) []rowT {
	_, rows := mm.specsAndRows(t)
	return rows
}

// selectedJob returns the highlighted job on the Jobs tab.
func (mm *m) selectedJob() (Job, bool) {
	if mm.tab != tabJobs {
		return Job{}, false
	}
	rows := mm.currentRows()
	if mm.sel[mm.tab] >= len(rows) || rows[mm.sel[mm.tab]].job == nil {
		return Job{}, false
	}
	return *rows[mm.sel[mm.tab]].job, true
}

// selectedResource returns the highlighted row as a model.Resource for console
// linking.
func (mm *m) selectedResource() (model.Resource, bool) {
	rows := mm.currentRows()
	if mm.sel[mm.tab] >= len(rows) {
		return model.Resource{}, false
	}
	r := rows[mm.sel[mm.tab]]
	return model.Resource{
		Service: "glue", Type: r.typ, Region: r.region, ID: r.name, ARN: r.arn,
	}, true
}
