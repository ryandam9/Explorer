package lambdatui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/sparkline"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The detail view (Enter) is one scrolling page. For a function: a header band
// (name, state, runtime · architecture · memory · timeout, description, age,
// ARN), a row of summary cards — Health, Usage · 30 days, Findings — and then
// the sections, each a titled panel at its natural height, packed into columns
// (every panel goes to the currently shortest column), so nothing is clipped
// inside a fixed tile and no tile sits half empty. Sections with nothing to
// show collapse into one "Not configured" card. Tab/Shift+Tab move focus
// between panels and scroll the page to the focused one; ↑/↓, PgUp/PgDn and
// g/G scroll; y copies the focused panel's text. Layers and event sources use
// the same page without the header cards.

// panelGap is the blank columns between side-by-side panels.
const panelGap = 1

// detailPanel is one titled panel of the page.
type detailPanel struct {
	title string
	body  string
}

// detailPage is the laid-out page: its text, and where each focusable panel
// starts (for Tab's scroll-into-view) with its plain text (for y).
type detailPage struct {
	content string
	tops    []int
	bodies  []string
}

// detailColCount picks the number of columns for the sections from the page
// width, never more than the panel count.
func detailColCount(n, width int) int {
	cols := 1
	switch {
	case width >= 170:
		cols = 3
	case width >= 110:
		cols = 2
	}
	return max(min(cols, n), 1)
}

// cardColCount is how many summary cards sit side by side: cards are compact,
// so they pair up on a width where the sections still stack.
func cardColCount(n, width int) int {
	cols := 1
	switch {
	case width >= 150:
		cols = 3
	case width >= 90:
		cols = 2
	}
	return max(min(cols, n), 1)
}

// panelSet returns the page's summary cards and sections. Only a function has
// cards; its empty sections fold into a "Not configured" card at the end.
func (mm *m) panelSet() (cards, secs []detailPanel) {
	isFunc := mm.detailFunc.Name != ""
	var empty []string
	for _, s := range mm.detailSections {
		if isFunc && s.Empty != "" {
			empty = append(empty, s.Empty)
			continue
		}
		secs = append(secs, detailPanel{title: s.Title, body: s.Body})
	}
	if len(empty) > 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
		var b strings.Builder
		for i, e := range empty {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(muted.Render("  · " + e))
		}
		secs = append(secs, detailPanel{title: "Not configured", body: b.String()})
	}
	if isFunc {
		cards = []detailPanel{
			{title: "Health", body: mm.healthCard()},
			{title: "Usage · 30 days", body: mm.usageCard()},
			{title: "Findings", body: mm.findingsCard()},
		}
	}
	return cards, secs
}

// buildDetailPage lays the page out at width columns.
func (mm *m) buildDetailPage(width int) detailPage {
	var page detailPage
	lines := strings.Split(mm.detailHeaderBand(width), "\n")
	cards, secs := mm.panelSet()
	focusIdx := 0
	add := func(p detailPanel, top int) bool {
		page.tops = append(page.tops, top)
		page.bodies = append(page.bodies, ansi.Strip(p.body))
		focused := focusIdx == mm.detailFocus
		focusIdx++
		return focused
	}

	// Summary cards: a row (or rows, when narrow) of equal-height panels.
	if len(cards) > 0 {
		lines = append(lines, "")
		per := cardColCount(len(cards), width)
		for lo := 0; lo < len(cards); lo += per {
			row := cards[lo:min(lo+per, len(cards))]
			widths := splitWidth(width, len(row))
			h := 0
			for i, c := range row {
				h = max(h, lipgloss.Height(ui.TitledPanel(c.title, c.body, widths[i], 0, false))-2)
			}
			top := len(lines)
			rendered := make([]string, len(row))
			for i, c := range row {
				rendered[i] = ui.TitledPanel(c.title, c.body, widths[i], h, add(c, top))
			}
			lines = append(lines, strings.Split(joinSideBySide(rendered, widths), "\n")...)
		}
	}

	// Sections: natural heights, each into the shortest column.
	if len(secs) > 0 {
		lines = append(lines, "")
		base := len(lines)
		cols := detailColCount(len(secs), width)
		widths := splitWidth(width, cols)
		colLines := make([][]string, cols)
		for _, s := range secs {
			c := shortestColumn(colLines)
			rendered := ui.TitledPanel(s.title, s.body, widths[c], 0, add(s, base+len(colLines[c])))
			colLines[c] = append(colLines[c], strings.Split(rendered, "\n")...)
		}
		rendered := make([]string, cols)
		for i := range colLines {
			rendered[i] = strings.Join(colLines[i], "\n")
		}
		lines = append(lines, strings.Split(joinSideBySide(rendered, widths), "\n")...)
	}
	page.content = strings.Join(lines, "\n")
	return page
}

// splitWidth divides width into n panel widths separated by panelGap, the
// last absorbing the rounding remainder.
func splitWidth(width, n int) []int {
	each := (width - panelGap*(n-1)) / n
	out := make([]int, n)
	for i := range out {
		out[i] = each
	}
	out[n-1] = width - (each+panelGap)*(n-1)
	return out
}

func shortestColumn(cols [][]string) int {
	best := 0
	for i := range cols {
		if len(cols[i]) < len(cols[best]) {
			best = i
		}
	}
	return best
}

// joinSideBySide places blocks next to each other, padding shorter ones with
// blank lines of their own width so every row lines up.
func joinSideBySide(blocks []string, widths []int) string {
	split := make([][]string, len(blocks))
	h := 0
	for i, b := range blocks {
		split[i] = strings.Split(b, "\n")
		h = max(h, len(split[i]))
	}
	gap := strings.Repeat(" ", panelGap)
	out := make([]string, h)
	for r := 0; r < h; r++ {
		var line strings.Builder
		for i := range split {
			if i > 0 {
				line.WriteString(gap)
			}
			if r < len(split[i]) {
				line.WriteString(split[i][r])
			} else {
				line.WriteString(strings.Repeat(" ", widths[i]))
			}
		}
		out[r] = line.String()
	}
	return strings.Join(out, "\n")
}

// --- header band and cards ---------------------------------------------------

// detailHeaderBand is the page's top: for a function, its name and state with
// the key configuration, then the description, age and ARN; otherwise the
// view's title.
func (mm *m) detailHeaderBand(width int) string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	d := mm.detailFunc
	if d.Name == "" {
		return heading.Render(" " + mm.detailTitle)
	}
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true)
	chips := []string{runtimeLabel(d.Runtime, d.PackageType), primaryArch(d.Architectures), formatMemory(d.MemoryMB), formatTimeout(d.TimeoutSec) + " timeout"}
	left := " " + accent.Render("λ") + " " + heading.Render(d.Name) + "   " + stateBadge(d.State) + "   " + muted.Render(strings.Join(chips, " · "))
	right := muted.Render(d.Region + " ")
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	line1 := ansi.Truncate(left, width, "…")
	if pad > 0 {
		line1 = left + strings.Repeat(" ", pad) + right
	}
	lines := []string{line1}
	if d.Description != "" {
		lines = append(lines, "   "+lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText())).Italic(true).Render(ansi.Truncate(d.Description, width-4, "…")))
	}
	meta := "modified " + shortTime(d.LastModified)
	if !d.LastModified.IsZero() {
		meta += " (" + ageLabel(d.LastModified, time.Now()) + ")"
	}
	lines = append(lines, muted.Render(ansi.Truncate("   "+meta+"  ·  "+d.ARN, width, "…")))
	return strings.Join(lines, "\n")
}

// stateBadge renders a function state in its colour: Active green, Failed red,
// Pending yellow, anything else muted.
func stateBadge(state string) string {
	c := ui.ColorMuted()
	switch strings.ToLower(state) {
	case "active", "successful", "enabled":
		c = ui.ColorSuccess()
	case "failed":
		c = ui.ColorError()
	case "pending", "inprogress", "creating", "updating":
		c = ui.ColorWarning()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Bold(true).Render(stateLabel(state))
}

func (mm *m) healthCard() string {
	d := mm.detailFunc
	var b strings.Builder
	b.WriteString(dkvStyledRaw("State", stateBadge(d.State)) + "\n")
	if d.StateReason != "" {
		b.WriteString(dkvWarn("Reason", d.StateReason) + "\n")
	}
	b.WriteString(dkvStyledRaw("Last update", stateBadge(d.LastUpdateStatus)) + "\n")
	if d.LastUpdateStatusReason != "" {
		b.WriteString(dkvWarn("Update reason", d.LastUpdateStatusReason) + "\n")
	}
	if d.ReservedConcurrency != nil && *d.ReservedConcurrency == 0 {
		b.WriteString(dkvWarn("Concurrency", reservedConcurrencyLabel(d.ReservedConcurrency)) + "\n")
	} else {
		b.WriteString(dkv("Concurrency", reservedConcurrencyLabel(d.ReservedConcurrency)) + "\n")
	}
	b.WriteString(dkv("Code", d.PackageType+" · "+formatCodeSize(d.CodeSize)))
	return b.String()
}

func (mm *m) usageCard() string {
	d := mm.detailFunc
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	if mm.usageLoading {
		return "  " + mm.spinner.View() + muted.Render(" reading usage and log groups…")
	}
	u, ok := mm.usage[usageKey(d.Region, d.Name)]
	if !ok {
		return muted.Render("  not loaded — press r on the list to refresh")
	}
	var b strings.Builder
	if !u.UsageKnown {
		b.WriteString(dkvWarn("Invocations", "? couldn't read CloudWatch metrics") + "\n")
	} else {
		count := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Bold(true).Render(formatCount(u.Invocations30d))
		b.WriteString("  " + count + muted.Render(" invocations") + "\n")
		if len(u.Daily) > 0 && u.Invocations30d > 0 {
			b.WriteString("  " + accent.Render(sparkline.Render(u.Daily)) + "\n")
			b.WriteString("  " + muted.Render(fmt.Sprintf("%-*s%s", len(u.Daily)-5, "30d ago", "today")) + "\n")
		}
		last := "none in 30 days"
		if !u.LastInvoked.IsZero() {
			last = u.LastInvoked.Format("2006-01-02") + " (" + ageLabel(u.LastInvoked, time.Now().UTC()) + ")"
		}
		b.WriteString(dkv("Last invoked", last) + "\n")
	}
	switch {
	case !u.LogKnown:
		b.WriteString(dkvWarn("Log group", "? couldn't read"))
	case !u.LogExists:
		b.WriteString(dkv("Log group", "none yet (never logged)"))
	case u.RetentionDays == 0:
		b.WriteString(dkvWarn("Log retention", "never expires · "+formatBytesShort(u.StoredBytes)))
	default:
		b.WriteString(dkv("Log retention", fmt.Sprintf("%d days · %s", u.RetentionDays, formatBytesShort(u.StoredBytes))))
	}
	return b.String()
}

func (mm *m) findingsCard() string {
	d := mm.detailFunc
	var mine []findings.Finding
	for _, f := range mm.computeFindings() {
		if f.Region == d.Region && f.Resource == d.Name {
			mine = append(mine, f)
		}
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	var b strings.Builder
	if len(mine) == 0 {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess())).Render("✓ no findings"))
	}
	for i, f := range mine {
		if i > 0 {
			b.WriteString("\n")
		}
		c := ui.ColorMuted()
		switch f.Severity {
		case findings.SevCritical:
			c = ui.ColorError()
		case findings.SevWarning:
			c = ui.ColorWarning()
		}
		glyph := strings.Fields(sevLabel(f.Severity))[0]
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render(glyph+" "+f.Title))
	}
	if mm.usageLoading {
		b.WriteString("\n" + muted.Render("  (usage & access checks still loading)"))
	}
	return b.String()
}

// dkvStyledRaw is dkv for a value that is already styled.
func dkvStyledRaw(label, styled string) string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	return "  " + muted.Render(fmt.Sprintf("%-18s", label)) + " " + styled
}

// ageLabel renders how long ago t was: "today", "yesterday", "26 days ago",
// "4 months ago", "3 years ago".
func ageLabel(t, now time.Time) string {
	days := int(math.Floor(now.Sub(t).Hours() / 24))
	switch {
	case days < 1:
		return "today"
	case days < 2:
		return "yesterday"
	case days < 60:
		return fmt.Sprintf("%d days ago", days)
	case days < 730:
		return fmt.Sprintf("%d months ago", days/30)
	}
	return fmt.Sprintf("%d years ago", days/365)
}

func formatBytesShort(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// --- the page viewport ---------------------------------------------------------

// detailPageHeight is the rows the page viewport gets: the frame minus the
// region badge and the status bar.
func (mm *m) detailPageHeight() int {
	h := mm.height - 1
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		h--
	}
	return max(h, 3)
}

// detailPageWidth leaves a gutter for the scrollbar.
func (mm *m) detailPageWidth() int {
	return max(mm.width-2, 30)
}

// focusDetailPanel moves focus by delta panels (wrapping) and scrolls the page
// so the focused panel's top is in view.
func (mm *m) focusDetailPanel(delta int) {
	page := mm.buildDetailPage(mm.detailPageWidth())
	n := len(page.tops)
	if n == 0 {
		return
	}
	mm.detailFocus = ((mm.detailFocus+delta)%n + n) % n
	top, h := page.tops[mm.detailFocus], mm.detailPageHeight()
	if top < mm.detailOffset || top > mm.detailOffset+h-4 {
		mm.detailOffset = max(top-1, 0)
	}
}

// scrollDetail moves the page by delta lines, clamped to its length.
func (mm *m) scrollDetail(delta int) {
	mm.detailOffset += delta
	mm.clampDetailOffset(lipgloss.Height(mm.buildDetailPage(mm.detailPageWidth()).content))
}

func (mm *m) clampDetailOffset(total int) {
	mm.detailOffset = max(min(mm.detailOffset, total-mm.detailPageHeight()), 0)
}

// focusedPanelText is the focused panel's plain text, for y.
func (mm *m) focusedPanelText() string {
	page := mm.buildDetailPage(mm.detailPageWidth())
	if mm.detailFocus < 0 || mm.detailFocus >= len(page.bodies) {
		return ""
	}
	return page.bodies[mm.detailFocus]
}

// renderDetail draws the page (or a spinner/error while a function's detail is
// in flight) with a scrollbar, filling the frame above the status bar.
func (mm *m) renderDetail() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(" " + mm.detailTitle)
	switch {
	case mm.detailLoading:
		return title + fmt.Sprintf("\n\n  %s Loading function configuration…", mm.spinner.View())
	case mm.detailErr != nil:
		return title + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load details: "+mm.detailErr.Error())
	case len(mm.detailSections) == 0:
		return title + "\n\n  No details available."
	}
	page := mm.buildDetailPage(mm.detailPageWidth())
	lines := strings.Split(page.content, "\n")
	h := mm.detailPageHeight()
	mm.clampDetailOffset(len(lines))
	end := min(mm.detailOffset+h, len(lines))
	visible := lines[mm.detailOffset:end]
	for len(visible) < h {
		visible = append(visible, "")
	}
	bar := strings.Split(ui.VScrollbar(h, len(lines), h, mm.detailOffset), "\n")
	w := mm.detailPageWidth()
	for i := range visible {
		pad := w - ansi.StringWidth(visible[i])
		visible[i] += strings.Repeat(" ", max(pad, 0)) + " "
		if i < len(bar) {
			visible[i] += bar[i]
		}
	}
	return strings.Join(visible, "\n")
}

// panelPageStep is how many lines PgUp/PgDn scroll the code browser's viewer.
const panelPageStep = 5

// detailHeading renders a view's top heading line.
func detailHeading(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(s)
}
