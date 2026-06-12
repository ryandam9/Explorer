package tui

// Global fuzzy finder (AXE-013): Ctrl+P opens a palette that fuzzy-matches
// across every collected resource — name, ID, ARN, type, region — and Enter
// jumps straight to it: the sidebar selects its service, the table cursor
// lands on its row, and the detail panel opens. The answer to "I have
// eni-0abc from an error message — what is it?".

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/fuzzy"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// finderMaxHits caps the ranked matches; more would scroll past the palette.
const finderMaxHits = 50

// finderVisibleRows is how many matches the palette lists at once.
const finderVisibleRows = 12

func (m *tuiModel) openFinder() {
	m.showFinder = true
	m.finderInput.SetValue("")
	m.finderInput.Focus()
	m.finderSel = 0
	m.computeFinderHits()
}

func (m *tuiModel) closeFinder() {
	m.showFinder = false
	m.finderInput.Blur()
}

// computeFinderHits ranks every resource against the query. searchText is
// the pre-joined, lower-cased cell text maintained parallel to sorted, so
// the query matches whatever the user could see in any column.
func (m *tuiModel) computeFinderHits() {
	query := strings.TrimSpace(m.finderInput.Value())
	hits := fuzzy.Rank(query, m.searchText, finderMaxHits)
	m.finderHits = m.finderHits[:0]
	for _, h := range hits {
		m.finderHits = append(m.finderHits, h.Index)
	}
	if m.finderSel >= len(m.finderHits) {
		m.finderSel = 0
	}
}

// jumpToResource navigates the main UI to sorted[idx]: service selected,
// filters cleared (they could hide the target), cursor on the row, detail
// open — exactly the state Enter on that row would have produced.
func (m *tuiModel) jumpToResource(idx int) {
	if idx < 0 || idx >= len(m.sorted) {
		return
	}
	res := m.sorted[idx]

	m.filterText = ""
	m.filterInput.SetValue("")
	m.filterRegion = ""
	m.filterState = ""

	m.activeService = 0
	for i, svc := range m.services {
		if svc == res.Service {
			m.activeService = i
			break
		}
		if svc == "All" {
			m.activeService = i // fallback while looking for the exact service
		}
	}

	m.invalidateRows()
	m.updateTableRows()

	svc := m.currentService()
	m.rowsFor(svc) // ensure the group cache is built
	for i, r := range m.allRes[svc] {
		if sameResource(r, res) {
			m.table.SetCursor(i)
			break
		}
	}

	m.detail = &res
	m.showDetail = true
	m.focus = focusDetail
	m.table.Blur()
	m.showTimeline = false
	m.showLogs = false
	m.showMetrics = false
	m.showXref = false
	m.syncTableLayout()
	m.syncDetailViewport()
}

// sameResource matches by ARN when either side has one, otherwise by the
// (service, ID, region) triple.
func sameResource(a, b model.Resource) bool {
	if a.ARN != "" || b.ARN != "" {
		return a.ARN == b.ARN
	}
	return a.Service == b.Service && a.ID == b.ID && a.Region == b.Region
}

// finderView renders the palette: query input, ranked matches with the
// selection highlighted, and a match count.
func (m tuiModel) finderView() string {
	w := m.width - 12
	if w > 76 {
		w = 76
	}
	if w < 40 {
		w = 40
	}
	inner := w - 4 // modal padding

	var b strings.Builder
	b.WriteString(ui.PanelTitleStyle().Render("Jump to resource") + "\n")
	b.WriteString(m.finderInput.View() + "\n\n")

	// Window the list around the selection.
	start := 0
	if m.finderSel >= finderVisibleRows {
		start = m.finderSel - finderVisibleRows + 1
	}
	end := start + finderVisibleRows
	if end > len(m.finderHits) {
		end = len(m.finderHits)
	}

	selStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ui.ColorHighlightText())).
		Background(lipgloss.Color(ui.ColorHighlight())).
		Bold(true)
	rowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorText()))

	if len(m.finderHits) == 0 {
		b.WriteString(ui.MutedStyle().Render("  no resources match") + "\n")
	}
	for i := start; i < end; i++ {
		line := finderLine(m.sorted[m.finderHits[i]], inner)
		if i == m.finderSel {
			b.WriteString(selStyle.Render("▸ "+line) + "\n")
		} else {
			b.WriteString(rowStyle.Render("  "+line) + "\n")
		}
	}

	b.WriteString("\n" + ui.MutedStyle().Render(fmt.Sprintf(
		"%d/%d resources · ↑/↓ select · Enter jump · Esc close",
		len(m.finderHits), len(m.sorted))))

	return ui.ModalStyle(w, finderVisibleRows+8).Render(b.String())
}

// finderLine formats one match: name (or ID), service/type, region — padded
// into fixed columns that fit the palette width.
func finderLine(r model.Resource, width int) string {
	name := r.Name
	if name == "" {
		name = r.ID
	}
	typ := r.Service
	if r.Type != "" {
		typ += "/" + r.Type
	}
	// Three columns: name gets whatever the type and region don't need.
	typeW, regionW := 22, 14
	nameW := width - typeW - regionW - 6
	if nameW < 12 {
		nameW = 12
	}
	return fmt.Sprintf("%-*.*s  %-*.*s  %-*.*s",
		nameW, nameW, name, typeW, typeW, typ, regionW, regionW, r.Region)
}
