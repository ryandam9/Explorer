package emrtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// The describe view (d) is a full-screen, btop-style grid of per-section panels
// (issue #285): overview, configuration/OS, compute/memory/storage, networking,
// instances and services each get their own bordered, independently scrollable
// tile. Tab/arrows move focus between tiles; the focused tile scrolls.

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
	if mm.descFocus < 0 || mm.descFocus >= len(mm.descPanels) {
		return nil
	}
	return &mm.descPanels[mm.descFocus]
}

// focusPanel moves focus to panel i, wrapping at both ends. It is a no-op in the
// single-pane fallback (a small terminal), where all sections share one viewport.
func (mm *m) focusPanel(i int) {
	n := len(mm.descPanels)
	if n == 0 {
		return
	}
	if _, _, single := mm.describeLayout(); single {
		mm.descFocus = 0
		return
	}
	if i < 0 {
		i = n - 1
	}
	if i >= n {
		i = 0
	}
	mm.descFocus = i
}

// describeLayout returns the grid height, column count, and whether the grid
// must fall back to a single scrolling pane. The fallback fires when the
// terminal is too short to give every stacked panel its minimum height — without
// it the panels would overflow and clip the status bar (issue #237 / rule #9).
func (mm *m) describeLayout() (gridH, cols int, single bool) {
	n := len(mm.descSections)
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
	cols = describeColCount(n, width)
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

// renderDescribe draws the full-screen describe view: a heading line plus the
// panel grid (or a spinner/error while loading). The grid is sized to fill the
// terminal below the heading and above the status bar (added by View()).
func (mm *m) renderDescribe() string {
	title := heading(" Describe — " + mm.detailCluster.Name)
	switch {
	case mm.descLoading:
		return title + fmt.Sprintf("\n\n  %s Describing cluster… gathering instance groups, storage, EC2 specs and VPC networking.", mm.spinner.View())
	case mm.descErr != nil:
		return title + "\n\n  " + errLine("Could not describe cluster: "+mm.descErr.Error())
	case len(mm.descSections) == 0:
		return title + "\n\n  No description available."
	}

	width := mm.width
	if width < minPanelWidth {
		width = minPanelWidth
	}
	gridH, cols, single := mm.describeLayout()
	if single {
		// Terminal too short for a tiled grid: one scrolling pane holds every
		// section so nothing is clipped and everything stays reachable.
		return title + "\n" + mm.renderSinglePane(gridH, width)
	}
	colSections := splitColumns(len(mm.descSections), cols)

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
			boxes = append(boxes, mm.renderPanel(si, thisW, heights[k]))
		}
		rendered = append(rendered, lipgloss.JoinVertical(lipgloss.Left, boxes...))
	}
	return title + "\n" + joinColumns(rendered, gap)
}

// renderSinglePane renders every section in one full-width scrolling box, used
// when the terminal is too short for the tiled grid. descPanels[0] backs the
// scroll so focusedPanel/scrollPanel keep working unchanged.
func (mm *m) renderSinglePane(gridH, width int) string {
	var b strings.Builder
	for i, s := range mm.descSections {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(heading(" "+s.Title) + "\n")
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
	off := mm.descPanels[0].YOffset
	vp := viewport.New(vpW, vpH)
	vp.SetContent(lipgloss.NewStyle().Width(vpW).Render(strings.TrimRight(b.String(), "\n")))
	vp.SetYOffset(off)
	mm.descPanels[0] = vp

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

// renderPanel sizes (preserving scroll offset) and renders one section tile to
// w×h cells, including its title, scrollbar and a focus-highlighted border.
func (mm *m) renderPanel(si, w, h int) string {
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
	off := mm.descPanels[si].YOffset
	vp := viewport.New(vpW, vpH)
	vp.SetContent(lipgloss.NewStyle().Width(vpW).Render(mm.descSections[si].Body))
	vp.SetYOffset(off)
	mm.descPanels[si] = vp

	bar := ui.VScrollbar(vp.Height, vp.TotalLineCount(), vp.VisibleLineCount(), vp.YOffset)
	content := lipgloss.JoinHorizontal(lipgloss.Top, vp.View(), " ", bar)

	focused := si == mm.descFocus
	titleColor := ui.ColorMuted()
	if focused {
		titleColor = ui.ColorHeading()
	}
	titleLine := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(titleColor)).
		Render(truncate(mm.descSections[si].Title, innerW))

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

// describeColCount picks the number of columns for the grid from the terminal
// width, never more than the section count.
func describeColCount(n, width int) int {
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
