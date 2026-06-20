package tagstui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// CoverageNote is the honest scope statement (CLAUDE.md §2): the Tagging API
// only sees tagged resources, and only services that integrate with it.
const CoverageNote = "Shows resources tagged & known to the Resource Groups Tagging API (untagged resources and unsupported services — e.g. IAM — won't appear)."

func (mm *m) View() string {
	if mm.width == 0 {
		return "Initializing…"
	}

	var b strings.Builder
	if badge := ui.RegionBadge(mm.regions, mm.allRegions); badge != "" {
		b.WriteString(badge + "\n")
	}
	b.WriteString(mm.header() + "\n")

	if mm.filterActive {
		b.WriteString(" " + mm.filter.View() + "\n")
	} else {
		b.WriteString(ui.MutedStyle().Render("  "+CoverageNote) + "\n")
	}

	b.WriteString(mm.columns())

	if note := mm.partialNote(); note != "" {
		b.WriteString("\n" + note)
	}

	body := b.String()
	status := ui.StatusBar(mm.width, mm.statusLeft(), mm.hints())
	sep := "\n"
	if mm.height > 0 {
		if n := mm.height - lipgloss.Height(body) - lipgloss.Height(status) + 1; n > 1 {
			sep = strings.Repeat("\n", n)
		}
	}
	frame := mm.applyToast(ui.ClipToSize(body+sep+status, mm.width, mm.height))
	if mm.showAbout {
		frame = ui.OverlayCenterBlank(ui.AboutView("About — AWS Tags", aboutText, ui.AboutWidth(mm.width)), mm.width, mm.height)
	}
	return frame
}

const aboutText = "Explore AWS resources by tag in a three-column layout: tag Keys ▸ the " +
	"selected key's Values ▸ Resources carrying the selected tag. Use ↑/↓ to move " +
	"within a column, Enter (or →) to drill into the next column, ←/Esc to step " +
	"back, and Tab to cycle focus. Values and resources load on demand, never on " +
	"scroll.\n\n" +
	"Or press f to type a filter directly: comma-separated Key=Value terms are " +
	"ANDed; repeat a key to OR its values; a bare key matches any value. Separate " +
	"groups with \"||\" to OR them (e.g. Team=payments || Team=billing). Scope to " +
	"resource types with a type:ec2:instance term. Note: only *tagged* resources " +
	"are queryable — there is no \"untagged\" filter.\n\n" +
	"Data comes from the Resource Groups Tagging API, so only tagged resources " +
	"on services that integrate with it are shown (IAM, for example, is not). Use " +
	"--all-regions to sweep every region. On a resource, y copies its ARN and o " +
	"opens it in the AWS console. r refreshes the focused column."

func (mm *m) header() string {
	crumb := "Tags ▸ Keys"
	switch mm.focus {
	case colValues:
		crumb = "Tags ▸ Keys ▸ " + mm.selectedKey
	case colResources:
		if mm.filterDesc != "" {
			crumb = "Tags ▸ " + mm.filterDesc
		}
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(" " + crumb)
}

// columns renders the three always-visible Miller columns side by side, the
// focused one highlighted (#333). Widths and heights are derived from the actual
// terminal size so nothing overruns the panels (§9).
func (mm *m) columns() string {
	const gap = 1
	keysW := mm.width * 24 / 100
	valuesW := mm.width * 28 / 100
	if keysW < 14 {
		keysW = 14
	}
	if valuesW < 16 {
		valuesW = 16
	}
	resW := mm.width - keysW - valuesW - 2*gap
	if resW < 18 {
		resW = 18
	}

	h := mm.colHeight()
	g := strings.Repeat(" ", gap)
	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		mm.columnView(colKeys, keysW, h),
		g,
		mm.columnView(colValues, valuesW, h),
		g,
		mm.columnView(colResources, resW, h),
	)
}

// colHeight is the table height shared by all three columns: terminal height
// minus the measured chrome (badge, header, note line, panel border+title,
// partial-failure note, status bar).
func (mm *m) colHeight() int {
	badge := 0
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		badge = 1
	}
	note := 0
	if mm.partialNote() != "" {
		note = 1
	}
	h := mm.height - badge - 1 /*header*/ - 1 /*note/filter*/ - 3 /*panel border+title*/ - note - 1 /*status*/
	if h < 3 {
		h = 3
	}
	return h
}

// columnView renders one bordered column: a title line over its table (or a
// loading / empty placeholder), padded to the shared height so the three panels
// line up.
func (mm *m) columnView(col focusCol, outerW, tableH int) string {
	focused := mm.focus == col
	innerW := outerW - 4 // border (2) + padding (2)
	if innerW < 6 {
		innerW = 6
	}

	var (
		tbl     *table.Model
		title   string
		empty   string
		loading bool
		items   int
	)
	switch col {
	case colKeys:
		tbl, loading, items = &mm.keysTbl, mm.loadingKeys, len(mm.keys)
		title = "Keys"
		empty = "No tag keys in scope."
	case colValues:
		tbl, loading, items = &mm.valuesTbl, mm.loadingValues, len(mm.values)
		title = "Values"
		if mm.selectedKey != "" {
			title = "Values · " + mm.selectedKey
			empty = "No values for this key."
		} else {
			empty = "Select a key →"
		}
	case colResources:
		tbl, loading, items = &mm.resTbl, mm.loadingResources, len(mm.resources)
		title = "Resources"
		if mm.filterDesc != "" {
			title = "Resources · " + mm.filterDesc
			empty = "No matching resources."
		} else {
			empty = "Select a value →"
		}
	}

	box := lipgloss.NewStyle().Width(innerW).Height(tableH)
	var body string
	switch {
	case loading:
		body = box.Render(fmt.Sprintf("%s Loading…", mm.spinner.View()))
	case items == 0:
		body = box.Render(ui.MutedStyle().Render(ansi.Truncate(empty, innerW, "…")))
	default:
		tbl.SetWidth(innerW)
		tbl.SetHeight(tableH)
		body = tbl.View()
	}

	content := lipgloss.JoinVertical(lipgloss.Left, mm.colTitle(title, focused, innerW), body)
	return ui.TablePanelStyle(focused).Render(content)
}

// colTitle renders a column's heading, truncated to the inner width and
// highlighted when the column is focused.
func (mm *m) colTitle(title string, focused bool, innerW int) string {
	color := ui.ColorHeading()
	if focused {
		color = ui.ColorBorderFocus()
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(color)).
		Width(innerW).
		Render(ansi.Truncate(title, innerW, "…"))
}

// partialNote flags per-region failures across the loaded columns so empty/short
// results aren't mistaken for "nothing there" (§6a).
func (mm *m) partialNote() string {
	seen := map[string]bool{}
	var regions []string
	for _, errs := range [][]model.ExploreError{mm.keysErrs, mm.valuesErrs, mm.resErrs} {
		for _, e := range errs {
			if e.Region != "" && !seen[e.Region] {
				seen[e.Region] = true
				regions = append(regions, e.Region)
			}
		}
	}
	if len(regions) == 0 {
		return ""
	}
	return ui.ErrorStyle().Render("  ⚠ " + fmt.Sprintf("%d region(s) failed (%s) — results may be incomplete; press r to retry",
		len(regions), strings.Join(regions, ", ")))
}

func (mm *m) statusLeft() string {
	region := "all (" + fmt.Sprintf("%d", len(mm.regions)) + " regions)"
	if len(mm.regions) == 1 {
		region = mm.regions[0]
	}
	switch mm.focus {
	case colValues:
		return fmt.Sprintf("Region: %s  ·  %s: %d values", region, mm.selectedKey, len(mm.values))
	case colResources:
		return fmt.Sprintf("Region: %s  ·  %d resources", region, len(mm.resources))
	default:
		return fmt.Sprintf("Region: %s  ·  %d tag keys", region, len(mm.keys))
	}
}

func (mm *m) hints() []ui.KeyHint {
	if mm.filterActive {
		return []ui.KeyHint{ui.H("Enter", "search"), ui.H("Esc", "cancel")}
	}
	base := []ui.KeyHint{ui.H("↑/↓", "rows"), ui.H("Enter/→", "drill"), ui.H("←", "back"), ui.H("Tab", "column")}
	if mm.focus == colResources {
		base = append(base, ui.H("</>", "scroll"), ui.H("y", "copy ARN"), ui.H("o", "console"))
	}
	base = append(base, ui.H("f", "filter"), ui.H("r", "refresh"), ui.H("i", "about"), ui.H("q", "quit"))
	return base
}

func (mm *m) applyToast(rendered string) string {
	if mm.toast == "" {
		return rendered
	}
	toast := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorSuccess())).
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Padding(0, 2).Bold(true).Render("✓ " + mm.toast)
	lines := strings.Split(rendered, "\n")
	if len(lines) >= 1 {
		lines[0] = lipgloss.PlaceHorizontal(mm.width, lipgloss.Right, toast)
	}
	return strings.Join(lines, "\n")
}
