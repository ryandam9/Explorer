package emrtui

import (
	"sort"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// Cluster column indices, matching the order built in clusterColumns. Used by
// the column sort so the comparator and the "default direction" logic stay in
// step with the rendered layout.
const (
	colName int = iota
	colID
	colState
	colRelease
	colApps
	colHRS
	colAge
	colRegion
)

// appsCap bounds the free-form APPLICATIONS column so a long application list
// can't dominate the table's width. The full text is available in the detail
// overlay. NAME is intentionally *not* capped: it is the frozen first column,
// so the shared table grows it to fit and scrolls the rest horizontally, and a
// cluster's full name is the most useful thing to read at a glance.
const appsCap = 22

// clusterColumns is the shared-table column set for the cluster list. Widths are
// floors: the table grows each column to fit its content and scrolls
// horizontally when the set overflows the panel. REGION is appended only when
// the scope spans more than one region.
func clusterColumns(multi bool) []table.Column {
	cols := []table.Column{
		{Title: "NAME", Width: 8},
		{Title: "ID", Width: 14},
		{Title: "STATE", Width: 12},
		{Title: "RELEASE", Width: 9},
		{Title: "APPLICATIONS", Width: 12},
		{Title: "HRS", Width: 4},
		{Title: "AGE", Width: 5},
	}
	if multi {
		cols = append(cols, table.Column{Title: "REGION", Width: 9})
	}
	return cols
}

// clusterRow renders one cluster as a shared-table row. State carries a glyph
// (✓/●/✗/•) so the row reads at a glance without per-cell colour, which the
// shared table does not apply. NAME is shown in full (frozen column + scroll);
// APPLICATIONS is capped (see appsCap).
func clusterRow(c Cluster, multi bool) table.Row {
	r := table.Row{
		c.Name,
		c.ID,
		stateLabel(c.State),
		c.ReleaseLabel,
		truncate(c.Applications, appsCap),
		instanceHours(c.InstanceHours),
		ageLabel(c.Created),
	}
	if multi {
		r = append(r, c.Region)
	}
	return r
}

func instanceHours(h int32) string {
	if h <= 0 {
		return "—"
	}
	return itoa(int(h))
}

// ageLabel renders how long ago a cluster was created as a compact "5d" / "3h"
// / "12m", or "—" when the creation time is unknown.
func ageLabel(created time.Time) string {
	if created.IsZero() {
		return "—"
	}
	d := time.Since(created)
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 24*time.Hour:
		return itoa(int(d.Hours())/24) + "d"
	case d >= time.Hour:
		return itoa(int(d.Hours())) + "h"
	case d >= time.Minute:
		return itoa(int(d.Minutes())) + "m"
	default:
		return "<1m"
	}
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

// buildView returns the clusters to display: the inventory filtered by the
// active filter term and ordered by the active column sort.
func (mm *m) buildView() []Cluster {
	term := strings.ToLower(strings.TrimSpace(mm.filter.Value()))
	out := make([]Cluster, 0, len(mm.inv.Clusters))
	for _, c := range mm.inv.Clusters {
		if term == "" || clusterMatches(c, term) {
			out = append(out, c)
		}
	}
	mm.sortClusters(out)
	return out
}

// sortClusters orders clusters in place by the selected column and direction.
// sortCol -1 leaves the natural order (already name/region sorted by
// Inventory.sort) untouched.
func (mm *m) sortClusters(cs []Cluster) {
	if mm.sortCol < 0 {
		return
	}
	col := mm.sortCol
	sort.SliceStable(cs, func(i, j int) bool {
		c := clusterCmp(cs[i], cs[j], col)
		if c == 0 {
			// Stable tiebreak so equal keys keep a predictable name order.
			c = strings.Compare(strings.ToLower(cs[i].Name), strings.ToLower(cs[j].Name))
		}
		if mm.sortAsc {
			return c < 0
		}
		return c > 0
	})
}

// clusterCmp compares two clusters by column, returning the usual -1/0/1. Text
// columns compare case-insensitively; HRS compares numerically.
func clusterCmp(a, b Cluster, col int) int {
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
	case colAge:
		// Compare by age (now − Created): a newer cluster has a smaller age, so
		// the ascending arrow reads youngest-first. Unknown (zero) creation times
		// sort as the oldest.
		switch {
		case a.Created.Equal(b.Created):
			return 0
		case a.Created.IsZero():
			return 1
		case b.Created.IsZero():
			return -1
		case a.Created.After(b.Created): // a is newer → smaller age → a < b
			return -1
		default:
			return 1
		}
	case colRegion:
		return strings.Compare(a.Region, b.Region)
	default: // colName
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	}
}

// clusterMatches reports whether the cluster matches term in any shown field.
func clusterMatches(c Cluster, term string) bool {
	hay := strings.ToLower(strings.Join([]string{
		c.Name, c.ID, c.State, c.ReleaseLabel, c.Applications, c.Region,
	}, " "))
	return strings.Contains(hay, term)
}

func (mm *m) rowCount() int { return len(mm.view) }

// selectedCluster returns the highlighted cluster.
func (mm *m) selectedCluster() (Cluster, bool) {
	i := mm.tbl.Cursor()
	if i < 0 || i >= len(mm.view) {
		return Cluster{}, false
	}
	return mm.view[i], true
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
