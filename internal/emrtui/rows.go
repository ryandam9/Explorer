package emrtui

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
// the remaining width; at most one flex column per table).
type colSpec struct {
	title string
	width int
}

// rowT is a rendered cluster row plus the source identity needed for actions
// (console links, step drill-down).
type rowT struct {
	cells   []cell
	cluster *Cluster
}

// specsAndRows returns the column layout and the unfiltered cluster rows.
func (mm *m) specsAndRows() ([]colSpec, []rowT) {
	multi := len(mm.regions) > 1
	// NAME is capped (not flexible) so the more important ID/STATE columns sit
	// close to it instead of being pushed right by a name-hogging column; the
	// variable-length APPLICATIONS list absorbs any leftover width instead.
	specs := []colSpec{
		{"NAME", 25}, {"ID", 14}, {"STATE", 22}, {"RELEASE", 11}, {"APPLICATIONS", 0}, {"HRS", 5},
	}
	rows := make([]rowT, 0, len(mm.inv.Clusters))
	for i := range mm.inv.Clusters {
		cl := mm.inv.Clusters[i]
		rows = append(rows, rowT{
			cells: []cell{
				{text: cl.Name},
				{text: cl.ID},
				{text: stateLabel(cl.State), color: stateColor(cl.State)},
				{text: cl.ReleaseLabel},
				{text: cl.Applications},
				{text: instanceHours(cl.InstanceHours)},
			},
			cluster: &mm.inv.Clusters[i],
		})
	}

	if multi {
		specs = append(specs, colSpec{"REGION", 14})
		for i := range rows {
			rows[i].cells = append(rows[i].cells, cell{text: rows[i].cluster.Region})
		}
	}
	return specs, rows
}

func instanceHours(h int32) string {
	if h <= 0 {
		return "—"
	}
	return itoa(int(h))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// currentRows returns the filtered cluster rows.
func (mm *m) currentRows() []rowT {
	_, rows := mm.specsAndRows()
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

// rowMatches reports whether any cell contains term.
func rowMatches(r rowT, term string) bool {
	for _, c := range r.cells {
		if strings.Contains(strings.ToLower(c.text), term) {
			return true
		}
	}
	return false
}

func (mm *m) rowCount() int { return len(mm.currentRows()) }

// selectedCluster returns the highlighted cluster.
func (mm *m) selectedCluster() (Cluster, bool) {
	rows := mm.currentRows()
	if mm.sel >= len(rows) || rows[mm.sel].cluster == nil {
		return Cluster{}, false
	}
	return *rows[mm.sel].cluster, true
}

// selectedResource returns the highlighted cluster as a model.Resource for
// console linking.
func (mm *m) selectedResource() (model.Resource, bool) {
	cl, ok := mm.selectedCluster()
	if !ok {
		return model.Resource{}, false
	}
	return model.Resource{
		Service: "emr", Type: "cluster", Region: cl.Region, ID: cl.ID, ARN: cl.ARN,
	}, true
}
