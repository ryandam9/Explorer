package emrtui

import (
	"sort"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// Cluster column indices, matching the order built in specsAndRows. Used by the
// column sort so the comparator and the "default direction" logic stay in step
// with the rendered layout.
const (
	colName int = iota
	colID
	colState
	colRelease
	colApps
	colHRS
	colRegion
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

// currentRows returns the filtered, sorted cluster rows.
func (mm *m) currentRows() []rowT {
	_, rows := mm.specsAndRows()
	term := strings.ToLower(strings.TrimSpace(mm.filter.Value()))
	if term != "" {
		var out []rowT
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

// sortRows orders rows in place by the selected column and direction. sortCol
// -1 leaves the natural order (already name/region sorted by Inventory.sort)
// untouched.
func (mm *m) sortRows(rows []rowT) {
	if mm.sortCol < 0 {
		return
	}
	col := mm.sortCol
	sort.SliceStable(rows, func(i, j int) bool {
		c := clusterCmp(rows[i].cluster, rows[j].cluster, col)
		if c == 0 {
			// Stable tiebreak so equal keys keep a predictable name order.
			c = strings.Compare(strings.ToLower(rows[i].cluster.Name), strings.ToLower(rows[j].cluster.Name))
		}
		if mm.sortAsc {
			return c < 0
		}
		return c > 0
	})
}

// clusterCmp compares two clusters by column, returning the usual -1/0/1. Text
// columns compare case-insensitively; HRS compares numerically.
func clusterCmp(a, b *Cluster, col int) int {
	switch col {
	case colID:
		return strings.Compare(a.ID, b.ID)
	case colState:
		return strings.Compare(a.State, b.State)
	case colRelease:
		return strings.Compare(a.ReleaseLabel, b.ReleaseLabel)
	case colApps:
		return strings.Compare(strings.ToLower(a.Applications), strings.ToLower(b.Applications))
	case colHRS:
		switch {
		case a.InstanceHours < b.InstanceHours:
			return -1
		case a.InstanceHours > b.InstanceHours:
			return 1
		default:
			return 0
		}
	case colRegion:
		return strings.Compare(a.Region, b.Region)
	default: // colName
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
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
