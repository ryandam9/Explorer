package lambdatui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// rowT is one rendered table row plus the source identity needed for actions
// (console links, detail drill-down). cells is the shared-table row; the rest
// identifies the underlying resource.
type rowT struct {
	cells  table.Row
	name   string
	region string
	arn    string
	typ    string
	fn     *Function
	layer  *Layer
	es     *EventSource
}

// tabColumns returns the shared-table column set for a tab. Widths are floors:
// the table grows each column to fit its content and scrolls horizontally when
// the set overflows. REGION is appended in multi-region scope.
func tabColumns(t tab, multi bool) []table.Column {
	var cols []table.Column
	switch t {
	case tabFunctions:
		cols = []table.Column{
			{Title: "NAME", Width: 16},
			{Title: "RUNTIME", Width: 12},
			{Title: "MEMORY", Width: 8},
			{Title: "TIMEOUT", Width: 8},
			{Title: "STATE", Width: 10},
			{Title: "LAST MODIFIED", Width: 16},
		}
	case tabLayers:
		cols = []table.Column{
			{Title: "NAME", Width: 18},
			{Title: "VERSION", Width: 8},
			{Title: "RUNTIMES", Width: 20},
		}
	case tabEventSources:
		cols = []table.Column{
			{Title: "FUNCTION", Width: 16},
			{Title: "SOURCE", Width: 22},
			{Title: "STATE", Width: 10},
			{Title: "BATCH", Width: 6},
		}
	}
	if multi {
		cols = append(cols, table.Column{Title: "REGION", Width: 9})
	}
	return cols
}

// tabRows builds the identity+row pairs for a tab.
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
	case tabFunctions:
		for i := range mm.inv.Functions {
			f := mm.inv.Functions[i]
			add(rowT{
				cells: table.Row{f.Name, runtimeLabel(f.Runtime, f.PackageType), formatMemory(f.MemoryMB), formatTimeout(f.TimeoutSec), stateLabel(f.State), shortTime(f.LastModified)},
				name:  f.Name, region: f.Region, arn: f.ARN, typ: "function", fn: &mm.inv.Functions[i],
			}, f.Region)
		}
	case tabLayers:
		for i := range mm.inv.Layers {
			l := mm.inv.Layers[i]
			add(rowT{
				cells: table.Row{l.Name, fmt.Sprintf("%d", l.LatestVersion), joinOrDash(l.Runtimes)},
				name:  l.Name, region: l.Region, arn: l.ARN, typ: "layer", layer: &mm.inv.Layers[i],
			}, l.Region)
		}
	case tabEventSources:
		for i := range mm.inv.EventSources {
			es := mm.inv.EventSources[i]
			add(rowT{
				cells: table.Row{es.FunctionName, es.SourceLabel, stateLabel(es.State), fmt.Sprintf("%d", es.BatchSize)},
				name:  es.FunctionName, region: es.Region, arn: es.ARN, typ: "event-source-mapping", es: &mm.inv.EventSources[i],
			}, es.Region)
		}
	}
	return rows
}

// tabCount returns how many resources a tab holds, read directly from the
// inventory so the tab bar can show per-tab counts every frame.
func (mm *m) tabCount(t tab) int {
	switch t {
	case tabFunctions:
		return len(mm.inv.Functions)
	case tabLayers:
		return len(mm.inv.Layers)
	case tabEventSources:
		return len(mm.inv.EventSources)
	}
	return 0
}

// buildView returns the active tab's rows, filtered by the active filter term
// and ordered by the active column sort.
func (mm *m) buildView() []rowT {
	rows := mm.tabRows(mm.tab)
	term := strings.ToLower(strings.TrimSpace(mm.filter.Value()))
	if term != "" {
		out := make([]rowT, 0, len(rows))
		for _, r := range rows {
			if rowMatches(r, term) {
				out = append(out, r)
			}
		}
		rows = out
	}
	mm.sortRows(rows)
	return rows
}

// sortRows orders rows in place by the selected column's displayed text.
// sortCol -1 leaves the natural (name, region) order untouched.
func (mm *m) sortRows(rows []rowT) {
	if mm.sortCol < 0 {
		return
	}
	col := mm.sortCol
	sort.SliceStable(rows, func(i, j int) bool {
		c := strings.Compare(strings.ToLower(cellAt(rows[i], col)), strings.ToLower(cellAt(rows[j], col)))
		if c == 0 {
			c = strings.Compare(strings.ToLower(cellAt(rows[i], 0)), strings.ToLower(cellAt(rows[j], 0)))
		}
		if mm.sortAsc {
			return c < 0
		}
		return c > 0
	})
}

func cellAt(r rowT, i int) string {
	if i < 0 || i >= len(r.cells) {
		return ""
	}
	return r.cells[i]
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

// selectedFunction returns the highlighted function on the Functions tab.
func (mm *m) selectedFunction() (Function, bool) {
	if mm.tab != tabFunctions {
		return Function{}, false
	}
	r, ok := mm.selectedRow()
	if !ok || r.fn == nil {
		return Function{}, false
	}
	return *r.fn, true
}

// selectedResource returns the highlighted row as a model.Resource for console
// linking.
func (mm *m) selectedResource() (model.Resource, bool) {
	r, ok := mm.selectedRow()
	if !ok {
		return model.Resource{}, false
	}
	return model.Resource{
		Service: "lambda", Type: r.typ, Region: r.region, ID: r.name, ARN: r.arn,
	}, true
}
