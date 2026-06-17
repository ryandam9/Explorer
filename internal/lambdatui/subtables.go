package lambdatui

import (
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newLambdaTable builds a focused shared-table with the given columns; the first
// column is pinned during horizontal scrolling.
func newLambdaTable(cols []table.Column) table.Model {
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
