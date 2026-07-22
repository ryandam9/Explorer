package s3tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// Text search inside the object preview overlay, mirroring the CloudWatch log
// viewer's in-page search (internal/cwtui/logviewer.go) so the two pages feel
// the same: a dedicated "Find:" line under the title, live highlighting while
// the term is typed (without scrolling), Enter jumps to the first match at or
// after the current scroll position, n/N step through matching lines
// (wrapping) with the current one marked "▸", and Esc in the input clears the
// search while Esc otherwise closes the preview. The search is purely visual —
// it never re-fetches the object — so it works the same for text, gzip and
// archive-member previews.
//
// A two-column gutter is reserved on every line unconditionally (the "▸"
// marker renders into it), so toggling the search never reflows the text.
const previewGutterWidth = 2

// computePreviewMatches returns the indices of the plain display lines that
// contain term, case-insensitively (the log viewer's computeMatches). A blank
// term matches nothing. Because the lines are the wrapped display lines, a
// term that spans a wrap boundary is not found — the same accepted limitation
// as the log viewer, which keeps matches and the scroll position in lockstep.
func computePreviewMatches(plain []string, term string) []int {
	t := strings.ToLower(term)
	if t == "" {
		return nil
	}
	var matches []int
	for i, l := range plain {
		if strings.Contains(strings.ToLower(l), t) {
			matches = append(matches, i)
		}
	}
	return matches
}

// termSpans returns the byte spans of every case-insensitive occurrence of
// term in line, in order. When case-folding changes the line's byte length
// (rare non-ASCII characters), it falls back to a case-sensitive scan so the
// spans always index line correctly rather than highlight the wrong bytes.
func termSpans(line, term string) [][2]int {
	if term == "" {
		return nil
	}
	hay, needle := strings.ToLower(line), strings.ToLower(term)
	if len(hay) != len(line) {
		hay, needle = line, term
	}
	var spans [][2]int
	for from := 0; ; {
		j := strings.Index(hay[from:], needle)
		if j < 0 {
			break
		}
		start := from + j
		spans = append(spans, [2]int{start, start + len(needle)})
		from = start + len(needle)
	}
	return spans
}

// highlightPreviewTerm renders a plain line with every occurrence of term
// highlighted (the log viewer's styleViewerLine, minus the severity tint).
func highlightPreviewTerm(line, term string) string {
	spans := termSpans(line, term)
	if len(spans) == 0 {
		return line
	}
	matchStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorHighlight())).
		Foreground(lipgloss.Color(ui.ColorHighlightText()))
	var b strings.Builder
	pos := 0
	for _, s := range spans {
		b.WriteString(line[pos:s[0]])
		b.WriteString(matchStyle.Render(line[s[0]:s[1]]))
		pos = s[1]
	}
	b.WriteString(line[pos:])
	return b.String()
}

// renderPreviewContent rebuilds the viewport content: every line carries the
// reserved gutter ("▸ " on the current match line, spaces elsewhere), and a
// line containing the term is re-rendered from its plain text with the
// occurrences highlighted — dropping that line's syntax colours — so the
// highlight spans, computed on plain text, can never land inside an ANSI
// escape sequence.
func renderPreviewContent(lines, plain []string, term string, matches []int, matchIdx int) string {
	cur := -1
	if term != "" && matchIdx < len(matches) {
		cur = matches[matchIdx]
	}
	marker := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render("▸ ")
	out := make([]string, len(lines))
	mi := 0
	for i, line := range lines {
		gutter := "  "
		if i == cur {
			gutter = marker
		}
		body := line
		if mi < len(matches) && matches[mi] == i {
			body = highlightPreviewTerm(plain[i], term)
			mi++
		}
		out[i] = gutter + body
	}
	return strings.Join(out, "\n")
}

// refreshPreviewContent re-renders the viewport content for the current search
// state. The scroll position is untouched.
func (m *Model) refreshPreviewContent() {
	if len(m.previewLines) == 0 {
		return
	}
	m.previewViewport.SetContent(renderPreviewContent(m.previewLines, m.previewPlain, m.previewSearchTerm, m.previewMatches, m.previewMatchIdx))
}

// startPreviewSearch gives the Find input the keyboard ("/"). Any previous
// term stays in the input for editing, exactly like the log viewer.
func (m *Model) startPreviewSearch() {
	m.previewSearching = true
	m.previewSearchInput.CursorEnd()
	m.previewSearchInput.Focus()
}

// setPreviewSearchTerm live-applies the typed term: matches recompute and
// highlight immediately, but the view does not scroll until Enter (mirroring
// the log viewer). The current-match index is kept when still valid so
// backspacing doesn't lose the user's place.
func (m *Model) setPreviewSearchTerm(term string) {
	m.previewSearchTerm = term
	m.previewMatches = computePreviewMatches(m.previewPlain, term)
	if m.previewMatchIdx >= len(m.previewMatches) {
		m.previewMatchIdx = 0
	}
	m.refreshPreviewContent()
}

// acceptPreviewSearch (Enter) closes the input keeping the term active for
// n/N, and jumps to the first match at or after the current scroll position
// (wrapping to the first match overall), like the log viewer's
// jumpToFirstMatchFrom.
func (m *Model) acceptPreviewSearch() {
	m.previewSearching = false
	m.previewSearchInput.Blur()
	if len(m.previewMatches) == 0 {
		return
	}
	m.previewMatchIdx = 0
	for i, line := range m.previewMatches {
		if line >= m.previewViewport.YOffset {
			m.previewMatchIdx = i
			break
		}
	}
	m.refreshPreviewContent()
	m.centerPreviewMatch()
}

// cancelPreviewSearch (Esc in the input) clears the search entirely: term,
// matches, highlights and the input's text.
func (m *Model) cancelPreviewSearch() {
	m.previewSearching = false
	m.previewSearchInput.Blur()
	m.previewSearchInput.SetValue("")
	m.previewSearchTerm = ""
	m.previewMatches = nil
	m.previewMatchIdx = 0
	m.refreshPreviewContent()
}

// stepPreviewMatch moves to the next (dir > 0) or previous (dir < 0) matching
// line, wrapping around, and centres it (n/N). A no-op with no matches.
func (m *Model) stepPreviewMatch(dir int) {
	n := len(m.previewMatches)
	if n == 0 {
		return
	}
	m.previewMatchIdx = (m.previewMatchIdx + dir + n) % n
	m.refreshPreviewContent()
	m.centerPreviewMatch()
}

// centerPreviewMatch scrolls so the current match line sits roughly
// mid-screen. SetYOffset clamps, so this never scrolls past either end.
func (m *Model) centerPreviewMatch() {
	if m.previewMatchIdx < len(m.previewMatches) {
		m.previewViewport.SetYOffset(m.previewMatches[m.previewMatchIdx] - m.previewViewport.Height/2)
	}
}

// previewFindLine renders the dedicated search line shown under the preview
// title — the same three states as the log viewer's search line: the input
// while typing, the active term with its match position, or a muted hint.
func (m *Model) previewFindLine() string {
	switch {
	case m.previewSearching:
		return "Find: " + m.previewSearchInput.View()
	case m.previewSearchTerm != "":
		pos := 0
		if len(m.previewMatches) > 0 {
			pos = m.previewMatchIdx + 1
		}
		return fmt.Sprintf("Find: %s  (%d/%d matches, n/N to jump)",
			lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Render(m.previewSearchTerm),
			pos, len(m.previewMatches))
	default:
		return ui.MutedStyle().Render("(Press / to search within the preview)")
	}
}
