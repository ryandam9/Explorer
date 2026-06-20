package tagstui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// coverageNote is the honest scope statement (CLAUDE.md §2): the Tagging API
// only sees tagged resources, and only services that integrate with it.
const coverageNote = "Shows resources tagged & known to the Resource Groups Tagging API (untagged resources and unsupported services — e.g. IAM — won't appear)."

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
		b.WriteString(ui.MutedStyle().Render("  "+coverageNote) + "\n")
	}

	b.WriteString(mm.body())

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

const aboutText = "Explore AWS resources by tag. Browse the account's tag keys, drill into a " +
	"key's values, and press Enter to list every resource carrying that tag — or " +
	"press f to type one or more Key=Value filters directly (comma-separated; " +
	"repeat a key to OR its values; a bare key matches any value).\n\n" +
	"Data comes from the Resource Groups Tagging API, so only tagged resources " +
	"on services that integrate with it are shown (IAM, for example, is not). Use " +
	"--all-regions to sweep every region. On a resource, y copies its ARN and o " +
	"opens it in the AWS console. r refreshes the current view."

func (mm *m) header() string {
	crumb := "Tags ▸ Keys"
	switch mm.pane {
	case paneValues:
		crumb = "Tags ▸ Keys ▸ " + mm.selectedKey
	case paneResources:
		crumb = "Tags ▸ " + mm.filterDesc
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(" " + crumb)
}

func (mm *m) body() string {
	if mm.loading {
		return fmt.Sprintf("\n  %s Loading…", mm.spinner.View())
	}

	var tbl = &mm.keysTbl
	empty := "No tag keys found in scope."
	switch mm.pane {
	case paneValues:
		tbl = &mm.valuesTbl
		empty = "No values found for " + mm.selectedKey + "."
	case paneResources:
		tbl = &mm.resTbl
		empty = "No resources match " + mm.filterDesc + "."
	}

	var b strings.Builder
	if (mm.pane == paneKeys && len(mm.keys) == 0) ||
		(mm.pane == paneValues && len(mm.values) == 0) ||
		(mm.pane == paneResources && len(mm.resources) == 0) {
		b.WriteString("\n  " + ui.MutedStyle().Render(empty))
	} else {
		mm.fit(tbl)
		b.WriteString(ui.TablePanelStyle(true).Render(tbl.View()))
		b.WriteString("\n" + ui.TableScrollIndicator(tbl))
	}
	if note := mm.partialNote(); note != "" {
		b.WriteString("\n" + note)
	}
	return b.String()
}

// partialNote flags per-region failures so empty/short results aren't mistaken
// for "nothing there" (§6a).
func (mm *m) partialNote() string {
	if len(mm.partial) == 0 {
		return ""
	}
	regions := make([]string, 0, len(mm.partial))
	for _, e := range mm.partial {
		regions = append(regions, e.Region)
	}
	return ui.ErrorStyle().Render("  ⚠ " + fmt.Sprintf("%d region(s) failed (%s) — results may be incomplete; press r to retry",
		len(mm.partial), strings.Join(regions, ", ")))
}

// fit sizes the active table to fill the space below the header/note and above
// the status bar (measured chrome, not assumed — §9).
func (mm *m) fit(tbl *table.Model) {
	if mm.width <= 0 || mm.height <= 0 {
		return
	}
	tbl.SetWidth(mm.width - 4)
	badge := 0
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		badge = 1
	}
	// chrome: region badge + header + note/filter line + scroll indicator +
	// status bar + the panel's top/bottom border.
	h := mm.height - badge - 1 - 1 - 1 - 1 - 2
	if h < 3 {
		h = 3
	}
	tbl.SetHeight(h)
}

func (mm *m) statusLeft() string {
	region := "all (" + fmt.Sprintf("%d", len(mm.regions)) + " regions)"
	if len(mm.regions) == 1 {
		region = mm.regions[0]
	}
	switch mm.pane {
	case paneValues:
		return fmt.Sprintf("Region: %s  ·  %s: %d values", region, mm.selectedKey, len(mm.values))
	case paneResources:
		return fmt.Sprintf("Region: %s  ·  %d resources", region, len(mm.resources))
	default:
		return fmt.Sprintf("Region: %s  ·  %d tag keys", region, len(mm.keys))
	}
}

func (mm *m) hints() []ui.KeyHint {
	if mm.filterActive {
		return []ui.KeyHint{ui.H("Enter", "search"), ui.H("Esc", "cancel")}
	}
	switch mm.pane {
	case paneResources:
		return []ui.KeyHint{
			ui.H("↑/↓", "rows"), ui.H("y", "copy ARN"), ui.H("o", "console"),
			ui.H("f", "filter"), ui.H("r", "refresh"), ui.H("Esc", "back"), ui.H("i", "about"), ui.H("q", "quit"),
		}
	default:
		return []ui.KeyHint{
			ui.H("↑/↓", "rows"), ui.H("Enter", "open"), ui.H("f", "filter"),
			ui.H("r", "refresh"), ui.H("Esc", "back"), ui.H("i", "about"), ui.H("q", "quit"),
		}
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
