package relatedtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

// caveat is the always-shown honesty note (§8): an empty side means none of the
// collected relationship types, not that the resource is isolated.
const caveat = "Only relationships this tool extracts are shown; an empty side ≠ isolated."

func linkRows(links []xref.Link) []table.Row {
	rows := make([]table.Row, len(links))
	for i, l := range links {
		name := l.Name
		if name == "" {
			name = l.ID
		}
		if name == "" {
			name = "—"
		}
		rows[i] = table.Row{fmt.Sprintf("%d", i+1), dash(l.Service), dash(l.Type), name, dash(l.Region), dash(l.Via)}
	}
	return rows
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (mm *m) View() string {
	if mm.width == 0 {
		return "Initializing…"
	}

	var b strings.Builder
	if badge := ui.RegionBadge(mm.regions, mm.allRegions); badge != "" {
		b.WriteString(badge + "\n")
	}
	b.WriteString(mm.header() + "\n")
	b.WriteString(ui.MutedStyle().Render("  "+caveat) + "\n")

	if mm.loading {
		b.WriteString(fmt.Sprintf("\n  %s Scanning the account for relationships…", mm.spinner.View()))
	} else {
		b.WriteString(mm.panels())
		if note := mm.partialNote(); note != "" {
			b.WriteString("\n" + note)
		}
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
	if mm.showHelp {
		frame = ui.OverlayCenterBlank(mm.helpOverlay(), mm.width, mm.height)
	}
	return frame
}

func (mm *m) header() string {
	crumb := "Related ▸ " + targetLabel(mm.stack)
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(" " + crumb)
}

// targetLabel renders the breadcrumb path as short names joined by ▸.
func targetLabel(stack []string) string {
	parts := make([]string, 0, len(stack))
	for _, s := range stack {
		parts = append(parts, shortLabel(s))
	}
	return strings.Join(parts, " ▸ ")
}

func shortLabel(id string) string {
	if i := strings.LastIndexAny(id, "/:"); i >= 0 && i+1 < len(id) {
		return id[i+1:]
	}
	return id
}

// panels renders the two stacked panels (Uses on top, Used by below), the
// focused one highlighted. Heights are derived from the terminal so nothing
// overruns (§9).
func (mm *m) panels() string {
	note := 0
	if mm.partialNote() != "" {
		note = 1
	}
	badge := 0
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		badge = 1
	}
	// chrome: badge + header + caveat + status + note; each panel adds border+title (3).
	avail := mm.height - badge - 1 - 1 - 1 - note
	if avail < 12 {
		avail = 12
	}
	half := avail / 2
	tableH := half - 3
	if tableH < 3 {
		tableH = 3
	}
	innerW := mm.width - 4
	if innerW < 10 {
		innerW = 10
	}

	uses := mm.panel(paneUses, fmt.Sprintf("Uses (depends on) →  ·  %d", len(mm.result.Uses)),
		&mm.usesTbl, len(mm.result.Uses), "Nothing this resource references was found.", innerW, tableH)
	usedBy := mm.panel(paneUsedBy, fmt.Sprintf("Used by ←  ·  %d", len(mm.result.UsedBy)),
		&mm.usedByTbl, len(mm.result.UsedBy), mm.usedByEmpty(), innerW, tableH)

	return lipgloss.JoinVertical(lipgloss.Left, uses, usedBy)
}

func (mm *m) panel(pane focusPane, title string, tbl *table.Model, items int, empty string, innerW, tableH int) string {
	focused := mm.focus == pane
	color := ui.ColorHeading()
	if focused {
		color = ui.ColorBorderFocus()
	}
	head := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).
		Width(innerW).Render(ansi.Truncate(title, innerW, "…"))

	var body string
	if items == 0 {
		body = lipgloss.NewStyle().Width(innerW).Height(tableH).Render(ui.MutedStyle().Render(empty))
	} else {
		tbl.SetWidth(innerW)
		tbl.SetHeight(tableH)
		body = tbl.View()
	}
	content := lipgloss.JoinVertical(lipgloss.Left, head, body)
	return ui.TablePanelStyle(focused).Render(content)
}

// usedByEmpty renders the scoped "not referenced" message listing the reference
// types checked for recognized kinds (§8).
func (mm *m) usedByEmpty() string {
	if len(mm.result.CheckedTypes) > 0 {
		return "Not referenced by anything checked: " + strings.Join(mm.result.CheckedTypes, ", ") + "."
	}
	return "Nothing that references this resource was found."
}

func (mm *m) partialNote() string {
	if len(mm.partial) == 0 {
		return ""
	}
	seen := map[string]bool{}
	var regions []string
	for _, e := range mm.partial {
		if e.Region != "" && !seen[e.Region] {
			seen[e.Region] = true
			regions = append(regions, e.Region)
		}
	}
	if len(regions) == 0 {
		return ""
	}
	return ui.ErrorStyle().Render("  ⚠ " + fmt.Sprintf("%d region(s)/source(s) failed (%s) — results may be incomplete; press r to retry",
		len(regions), strings.Join(regions, ", ")))
}

func (mm *m) statusLeft() string {
	region := "all (" + fmt.Sprintf("%d", len(mm.regions)) + " regions)"
	if len(mm.regions) == 1 {
		region = mm.regions[0]
	} else if mm.allRegions {
		region = "all regions"
	}
	return fmt.Sprintf("Region: %s  ·  hop %d", region, len(mm.stack))
}

func (mm *m) hints() []ui.KeyHint {
	if mm.loading {
		return []ui.KeyHint{ui.H("q", "quit")}
	}
	return []ui.KeyHint{
		ui.H("↑/↓", "rows"), ui.H("Tab/←/→", "pane"), ui.H("Enter", "open link"),
		ui.H("Esc", "back"), ui.H("y", "copy ARN"), ui.H("o", "console"),
		ui.H("r", "refresh"), ui.H("i", "help"), ui.H("q", "quit"),
	}
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

// --- help overlay -------------------------------------------------------------

func (mm *m) helpOverlay() string {
	mm.layoutHelpVP()
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).Render("↑/↓ scroll · i/Esc close")
	body := lipgloss.JoinVertical(lipgloss.Left, mm.overlayVP.View(), "", hint)
	return ui.HelpView("Help — Related resources", body, mm.overlayVP.Width+4)
}

func (mm *m) layoutHelpVP() {
	w := ui.AboutWidth(mm.width) - 4
	if w < 28 {
		w = 28
	}
	h := mm.height - 12
	if h < 6 {
		h = 6
	}
	off := mm.overlayVP.YOffset
	mm.overlayVP = viewport.New(w, h)
	mm.overlayVP.SetContent(lipgloss.NewStyle().Width(w).Render(helpContent()))
	mm.overlayVP.SetYOffset(off)
}

func helpContent() string {
	head := func(s string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorAccent())).Render(s)
	}
	var b strings.Builder
	w := func(lines ...string) {
		for _, l := range lines {
			b.WriteString(l + "\n")
		}
	}
	w(
		"Walk the relationships around a resource. Two panels are shown:",
		"",
		"    Uses (depends on) →     what this resource references",
		"    Used by ←               what references this resource",
		"",
		head("MOVING AROUND"),
		"  ↑ ↓ / k j     move within the focused panel",
		"  Tab           switch panel  (←/→ also select Uses / Used by)",
		"  Enter         re-center on the selected linked resource",
		"  Esc / ⌫       step back to the previous resource (breadcrumb)",
		"  < / >         scroll a wide table sideways",
		"",
		dimNote(),
		"",
		head("ON A ROW"),
		"  y             copy the resource's ARN",
		"  o             open it in the AWS console",
		"",
		head("OTHER"),
		"  r             re-scan the account        i  this help        q  quit",
		"",
		head("SCOPE & HONESTY"),
		"Edges are collected once and walked in memory — navigation never hits",
		"AWS. Only the relationship types this tool extracts are shown, so an",
		"empty side means \"none of those\", never \"isolated\"; the Used-by side",
		"lists the reference types it checked. Failed regions/sources are flagged.",
	)
	return strings.TrimRight(b.String(), "\n")
}

func dimNote() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted())).
		Render("Each Enter is one hop; the breadcrumb at the top shows your path.")
}
