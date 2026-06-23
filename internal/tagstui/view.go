package tagstui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
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
	if mm.showTags {
		frame = ui.OverlayCenterBlank(mm.tagsOverlay(), mm.width, mm.height)
	}
	if mm.showAbout {
		frame = ui.OverlayCenterBlank(mm.helpOverlay(), mm.width, mm.height)
	}
	return frame
}

// tagsOverlay renders the scrollable popup listing every tag on the resource the
// cursor is on (Enter on the Resources column). Scrollable so a heavily-tagged
// resource is fully reachable on any terminal height (§9).
func (mm *m) tagsOverlay() string {
	mm.layoutTagsVP()
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("↑/↓ scroll · Esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, mm.tagsVP.View(), "", hint)
	title := "Tags — " + mm.tagsResTitle()
	return ui.HelpView(title, body, mm.tagsVP.Width+4)
}

// tagsResTitle is the short resource label shown in the popup title.
func (mm *m) tagsResTitle() string {
	r := mm.tagsRes
	if r.Name != "" {
		return r.Name
	}
	if r.ID != "" {
		return r.ID
	}
	return r.Type
}

// layoutTagsVP sizes the tags viewport to the terminal (preserving the scroll
// offset) and renders the tag list wrapped to its width.
func (mm *m) layoutTagsVP() {
	w := ui.AboutWidth(mm.width) - 4 // the box pads 2 columns on each side
	if w < 28 {
		w = 28
	}
	h := mm.height - 12 // border + padding + title + hint + centering margins
	if h < 6 {
		h = 6
	}
	off := mm.tagsVP.YOffset
	mm.tagsVP = viewport.New(w, h)
	mm.tagsVP.SetContent(mm.tagsListContent(w))
	mm.tagsVP.SetYOffset(off)
}

// tagsListContent builds the popup body: an identifying sub-header plus every
// tag as "key = value", keys sorted for a deterministic, scannable list. Long
// values are soft-wrapped to the viewport width (§12).
func (mm *m) tagsListContent(w int) string {
	r := mm.tagsRes
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent()))

	var b strings.Builder
	meta := r.Type
	if r.Region != "" {
		meta += "  ·  " + r.Region
	}
	b.WriteString(dim.Render(ansi.Truncate(meta, w, "…")) + "\n\n")

	keys := make([]string, 0, len(r.Tags))
	for k := range r.Tags {
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		b.WriteString(dim.Render("No tags on this resource."))
		return b.String()
	}
	sort.Strings(keys)
	for _, k := range keys {
		line := keyStyle.Render(k) + dim.Render(" = ") + r.Tags[k]
		b.WriteString(lipgloss.NewStyle().Width(w).Render(line) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// helpOverlay renders the scrollable help modal (toggled with i). Scrollable so
// the full guide is reachable on any terminal height without overrunning the
// screen (§9).
func (mm *m) helpOverlay() string {
	mm.layoutHelpVP()
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("↑/↓ scroll · i/Esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, mm.overlayVP.View(), "", hint)
	return ui.HelpView("Help — AWS Tags explorer", body, mm.overlayVP.Width+4)
}

// layoutHelpVP sizes the help viewport to the terminal (preserving the scroll
// offset) and wraps the guide to its width so nothing runs off the edge.
func (mm *m) layoutHelpVP() {
	w := ui.AboutWidth(mm.width) - 4 // the box pads 2 columns on each side
	if w < 28 {
		w = 28
	}
	h := mm.height - 12 // border + padding + title + hint + centering margins
	if h < 6 {
		h = 6
	}
	off := mm.overlayVP.YOffset
	mm.overlayVP = viewport.New(w, h)
	mm.overlayVP.SetContent(lipgloss.NewStyle().Width(w).Render(helpContent()))
	mm.overlayVP.SetYOffset(off)
}

// helpContent builds the full, sectioned guide shown in the help overlay. Pure
// text (themed section headings); the viewport wraps it to width.
func helpContent() string {
	head := func(s string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent())).Render(s)
	}
	dim := func(s string) string {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render(s)
	}
	var b strings.Builder
	w := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}

	w(
		"Find AWS resources by their tags. Three columns are shown side by side:",
		"",
		"    Keys   ▸   Values   ▸   Resources",
		"",
		"Pick a tag key to load its values, then pick a value to list every",
		"resource carrying that Key=Value tag. The focused column has a",
		"highlighted border.",
		"",
		head("MOVING AROUND"),
		"  ↑ ↓  /  k j     move within the focused column",
		"  g / G           jump to the first / last row",
		"  Enter  or  →    drill into the next column (loads on demand)",
		"  ←  or  Esc      step focus back one column",
		"  Tab / Shift+Tab cycle focus across the three columns",
		"",
		dim("Values and resources are fetched only when you press Enter —"),
		dim("never while you scroll, so browsing stays fast."),
		"",
		head("THE \"Res\" COUNT"),
		"Each key and value shows how many resources carry it, filled in as",
		"background counts finish:",
		"  …      still counting",
		"  12     exact count",
		"  12+    a region's count failed — there are at least this many",
		"",
		head("FILTER MODE   (press f or /)"),
		"Type tag terms to jump straight to resources. Combine them:",
		"",
		"  Env=prod                 the exact tag",
		"  Env=prod, Team=pay       AND — both tags must match (across keys)",
		"  Team=pay, Team=billing   OR within a key — Team is pay or billing",
		"  Owner                    key present with any value (bare key)",
		"  Team=pay || Env=prod     OR across groups, joined with ||",
		"  type:ec2:instance        limit to a resource type",
		"",
		dim("Rule of thumb: a comma means AND; repeating a key, or || between"),
		dim("groups, means OR. Press Enter to run the filter (it fills the"),
		dim("Resources column); Esc cancels. Only tagged resources are"),
		dim("queryable — there is no \"untagged\" filter."),
		"",
		head("ON A RESOURCE   (Resources column)"),
		"  y        copy its ARN to the clipboard",
		"  o        open it in the AWS console",
		"  < / >    scroll the wide table sideways (more columns)",
		"",
		head("OTHER KEYS"),
		"  r        refresh the focused column (re-query AWS)",
		"  i        toggle this help        q   quit",
		"",
		head("COVERAGE"),
		"Data comes from the Resource Groups Tagging API, so only tagged",
		"resources on services that support it appear (IAM, for example,",
		"won't). Use --all-regions to sweep every region; global resources",
		"such as CloudFront and Route 53 appear under us-east-1.",
	)
	return strings.TrimRight(b.String(), "\n")
}

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
		base = append(base, ui.H("Enter", "tags"), ui.H("</>", "scroll"), ui.H("y", "copy ARN"), ui.H("o", "console"))
	}
	base = append(base, ui.H("f", "filter"), ui.H("r", "refresh"), ui.H("i", "help"), ui.H("q", "quit"))
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
