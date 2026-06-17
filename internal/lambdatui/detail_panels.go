package lambdatui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The detail view (Enter) is a full-screen, btop-style grid of per-section
// panels: overview, resources, state, VPC, environment, layers, code and tags
// each get their own bordered, independently scrollable tile. Tab/arrows move
// focus between tiles; the focused tile scrolls. This mirrors the EMR describe
// view so the two dashboards behave identically.

// panelPageStep is how many lines a PgUp/PgDn scrolls the focused panel.
const panelPageStep = 5

// minPanelWidth / minPanelHeight floor a tile so a tiny terminal still renders
// something legible rather than collapsing the borders.
const (
	minPanelWidth  = 24
	minPanelHeight = 4
)

// focusedPanel returns the focused section's viewport, or nil when there are no
// panels (still loading / errored).
func (mm *m) focusedPanel() *viewport.Model {
	if mm.detailFocus < 0 || mm.detailFocus >= len(mm.detailPanels) {
		return nil
	}
	return &mm.detailPanels[mm.detailFocus]
}

// focusPanel moves focus to panel i, wrapping at both ends. It is a no-op in the
// single-pane fallback (a small terminal), where all sections share one viewport.
func (mm *m) focusPanel(i int) {
	n := len(mm.detailPanels)
	if n == 0 {
		return
	}
	if _, _, single := mm.detailLayout(); single {
		mm.detailFocus = 0
		return
	}
	if i < 0 {
		i = n - 1
	}
	if i >= n {
		i = 0
	}
	mm.detailFocus = i
}

// detailLayout returns the grid height, column count, and whether the grid must
// fall back to a single scrolling pane. The fallback fires when the terminal is
// too short to give every stacked panel its minimum height — without it the
// panels would overflow and clip the status bar (rule #9).
func (mm *m) detailLayout() (gridH, cols int, single bool) {
	n := len(mm.detailSections)
	chrome := 2 // heading + status bar
	if ui.RegionBadge(mm.regions, mm.allRegions) != "" {
		chrome++
	}
	gridH = mm.height - chrome
	if gridH < minPanelHeight {
		gridH = minPanelHeight
	}
	width := mm.width
	if width < minPanelWidth {
		width = minPanelWidth
	}
	cols = detailColCount(n, width)
	if n == 0 {
		return gridH, cols, true
	}
	perCol := (n + cols - 1) / cols
	single = gridH < perCol*minPanelHeight
	return gridH, cols, single
}

// scrollPanel scrolls the focused panel by delta lines (negative = up).
func (mm *m) scrollPanel(delta int) {
	p := mm.focusedPanel()
	if p == nil {
		return
	}
	if delta < 0 {
		p.LineUp(-delta)
	} else {
		p.LineDown(delta)
	}
}

// renderDetail draws the full-screen detail view: a heading line plus the panel
// grid (or a spinner/error while a function's GetFunction is in flight). The
// grid is sized to fill the terminal below the heading and above the status bar.
func (mm *m) renderDetail() string {
	title := detailHeading(" " + mm.detailTitle)
	switch {
	case mm.detailLoading:
		return title + fmt.Sprintf("\n\n  %s Loading function configuration…", mm.spinner.View())
	case mm.detailErr != nil:
		return title + "\n\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError())).
			Render("Could not load details: "+mm.detailErr.Error())
	case len(mm.detailSections) == 0:
		return title + "\n\n  No details available."
	}

	width := mm.width
	if width < minPanelWidth {
		width = minPanelWidth
	}
	gridH, cols, single := mm.detailLayout()
	if single {
		return title + "\n" + mm.renderDetailSinglePane(gridH, width)
	}
	colSections := splitColumns(len(mm.detailSections), cols)

	const gap = 2
	colW := (width - gap*(cols-1)) / cols
	if colW < minPanelWidth {
		colW = minPanelWidth
	}

	rendered := make([]string, 0, cols)
	for ci, idxs := range colSections {
		thisW := colW
		if ci == cols-1 { // last column absorbs the rounding remainder
			thisW = width - (colW+gap)*(cols-1)
			if thisW < minPanelWidth {
				thisW = minPanelWidth
			}
		}
		heights := distribute(gridH, len(idxs))
		boxes := make([]string, 0, len(idxs))
		for k, si := range idxs {
			boxes = append(boxes, mm.renderDetailPanel(si, thisW, heights[k]))
		}
		rendered = append(rendered, lipgloss.JoinVertical(lipgloss.Left, boxes...))
	}
	return title + "\n" + joinColumns(rendered, gap)
}

// renderDetailSinglePane renders every section in one full-width scrolling box,
// used when the terminal is too short for the tiled grid. detailPanels[0] backs
// the scroll so focusedPanel/scrollPanel keep working unchanged.
func (mm *m) renderDetailSinglePane(gridH, width int) string {
	var b strings.Builder
	for i, s := range mm.detailSections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(detailHeading(" "+s.Title) + "\n")
		b.WriteString(s.Body)
	}
	innerW := width - 4
	if innerW < 6 {
		innerW = 6
	}
	vpW := innerW - 2
	if vpW < 4 {
		vpW = 4
	}
	vpH := gridH - 2
	if vpH < 1 {
		vpH = 1
	}
	off := mm.detailPanels[0].YOffset
	vp := viewport.New(vpW, vpH)
	vp.SetContent(lipgloss.NewStyle().Width(vpW).Render(strings.TrimRight(b.String(), "\n")))
	vp.SetYOffset(off)
	mm.detailPanels[0] = vp

	bar := ui.VScrollbar(vp.Height, vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset)
	content := lipgloss.JoinHorizontal(lipgloss.Top, vp.View(), " ", bar)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ui.ColorBorderFocus())).
		Width(width-2).
		Height(gridH-2).
		Padding(0, 1).
		Render(content)
}

// renderDetailPanel sizes (preserving scroll offset) and renders one section
// tile to w×h cells, including its title, scrollbar and a focus-highlighted
// border.
func (mm *m) renderDetailPanel(si, w, h int) string {
	if h < minPanelHeight {
		h = minPanelHeight
	}
	innerW := w - 4 // border (2) + horizontal padding (2)
	if innerW < 6 {
		innerW = 6
	}
	vpW := innerW - 2 // reserve a scrollbar gutter (bar + space) unconditionally
	if vpW < 4 {
		vpW = 4
	}
	vpH := h - 3 // border (2) + title line (1)
	if vpH < 1 {
		vpH = 1
	}

	// Re-size and re-fill the viewport, preserving its scroll offset across
	// renders/resizes; wrap the body to vpW so long values (ARNs) fold.
	off := mm.detailPanels[si].YOffset
	vp := viewport.New(vpW, vpH)
	vp.SetContent(lipgloss.NewStyle().Width(vpW).Render(mm.detailSections[si].Body))
	vp.SetYOffset(off)
	mm.detailPanels[si] = vp

	bar := ui.VScrollbar(vp.Height, vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset)
	content := lipgloss.JoinHorizontal(lipgloss.Top, vp.View(), " ", bar)

	focused := si == mm.detailFocus
	titleColor := ui.ColorMuted()
	if focused {
		titleColor = ui.ColorHeading()
	}
	titleLine := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).
		Render(truncate(mm.detailSections[si].Title, innerW))

	borderColor := ui.ColorBorder()
	if focused {
		borderColor = ui.ColorBorderFocus()
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Width(w-2).
		Height(h-2).
		Padding(0, 1).
		Render(lipgloss.JoinVertical(lipgloss.Left, titleLine, content))
}

// detailHeading renders the detail view's top heading line.
func detailHeading(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ui.ColorHeading())).Render(s)
}

// detailColCount picks the number of columns for the grid from the terminal
// width, never more than the section count.
func detailColCount(n, width int) int {
	cols := 1
	switch {
	case width >= 170:
		cols = 3
	case width >= 110:
		cols = 2
	}
	if cols > n {
		cols = n
	}
	if cols < 1 {
		cols = 1
	}
	return cols
}

// splitColumns assigns n section indices to cols columns contiguously (so the
// reading order is preserved top-to-bottom, left-to-right), balanced to within
// one panel per column.
func splitColumns(n, cols int) [][]int {
	sizes := distribute(n, cols)
	out := make([][]int, 0, cols)
	idx := 0
	for _, s := range sizes {
		col := make([]int, 0, s)
		for k := 0; k < s; k++ {
			col = append(col, idx)
			idx++
		}
		out = append(out, col)
	}
	return out
}

// distribute splits total into n nearly-equal parts (the first remainder parts
// get one extra), e.g. distribute(7,2) = [4,3].
func distribute(total, n int) []int {
	if n <= 0 {
		return nil
	}
	base, rem := total/n, total%n
	out := make([]int, n)
	for i := range out {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

// joinColumns places the rendered columns side by side with a gap of blank
// columns between them.
func joinColumns(cols []string, gap int) string {
	if len(cols) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cols)*2-1)
	spacer := strings.Repeat(" ", gap)
	for i, c := range cols {
		if i > 0 {
			parts = append(parts, spacer)
		}
		parts = append(parts, c)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}
