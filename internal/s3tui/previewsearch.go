package s3tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// Text search inside the object preview overlay ("/"). The query is matched
// case-insensitively against the *displayed* (already wrapped) lines, every
// occurrence is highlighted, and n/N step through the matches with the current
// one scrolled into view. The search is purely visual — it never re-fetches the
// object — so it works the same for text, gzip and archive-member previews.

// previewMatch is one occurrence of the query: a display-line index and the
// byte offsets of the match within that line's *plain* (ANSI-stripped) text.
type previewMatch struct {
	line  int
	start int
	end   int
}

// findPreviewMatches locates every case-insensitive occurrence of query in the
// plain display lines, in (line, offset) order. A blank query matches nothing.
// Offsets index the plain text, so a match can be highlighted without drifting
// inside the styled line's ANSI sequences. Because the lines are the wrapped
// display lines, a match that spans a wrap boundary is not found — an accepted
// limitation that keeps match positions and the viewport in lockstep.
func findPreviewMatches(plain []string, query string) []previewMatch {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	q := strings.ToLower(query)
	var matches []previewMatch
	for i, line := range plain {
		hay, needle := strings.ToLower(line), q
		if len(hay) != len(line) {
			// Case-folding changed the line's byte length (rare non-ASCII
			// characters), so lowered offsets would not map back onto the
			// original text. Fall back to a case-sensitive scan for this line
			// rather than highlight the wrong bytes.
			hay, needle = line, query
		}
		for from := 0; ; {
			j := strings.Index(hay[from:], needle)
			if j < 0 {
				break
			}
			start := from + j
			matches = append(matches, previewMatch{line: i, start: start, end: start + len(needle)})
			from = start + len(needle)
		}
	}
	return matches
}

// renderPreviewSearchContent rebuilds the viewport content with the matches
// highlighted (the current one in a distinct style). Lines without a match keep
// their syntax-highlighted form; a line containing matches is re-rendered from
// its plain text — dropping that line's syntax colours — so the highlight
// offsets, computed on plain text, can never land inside an ANSI sequence.
func renderPreviewSearchContent(lines, plain []string, matches []previewMatch, cur int) string {
	if len(matches) == 0 {
		return strings.Join(lines, "\n")
	}
	curStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent())).Reverse(true).Bold(true)
	otherStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning())).Reverse(true)

	out := make([]string, len(lines))
	copy(out, lines)
	for i := 0; i < len(matches); {
		li := matches[i].line
		j := i
		for j < len(matches) && matches[j].line == li {
			j++
		}
		src := plain[li]
		var b strings.Builder
		pos := 0
		for k := i; k < j; k++ {
			mm := matches[k]
			b.WriteString(src[pos:mm.start])
			style := otherStyle
			if k == cur {
				style = curStyle
			}
			b.WriteString(style.Render(src[mm.start:mm.end]))
			pos = mm.end
		}
		b.WriteString(src[pos:])
		out[li] = b.String()
		i = j
	}
	return strings.Join(out, "\n")
}

// startPreviewSearch opens the search prompt over the text preview, pre-filling
// the previous query (cursor at the end) so refining a search is quick.
func (m *Model) startPreviewSearch() {
	m.previewSearching = true
	m.previewSearchInput.SetValue(m.previewSearchQuery)
	m.previewSearchInput.CursorEnd()
	m.previewSearchInput.Focus()
	m.searchPreview(m.previewSearchInput.Value())
}

// searchPreview recomputes the matches for query, re-renders the highlights,
// and jumps to the first match. Called on every keystroke of the prompt so the
// highlight tracks the query live (mirroring the ":" jump prompt).
func (m *Model) searchPreview(query string) {
	m.previewSearchQuery = query
	m.previewMatches = findPreviewMatches(m.previewPlain, query)
	m.previewMatchIdx = 0
	m.refreshPreviewSearch()
}

// stepPreviewMatch moves the current match forward (dir > 0) or backward
// (dir < 0), wrapping around, and scrolls it into view.
func (m *Model) stepPreviewMatch(dir int) {
	n := len(m.previewMatches)
	if n == 0 {
		return
	}
	m.previewMatchIdx = ((m.previewMatchIdx+dir)%n + n) % n
	m.refreshPreviewSearch()
}

// refreshPreviewSearch re-renders the viewport content with the current match
// set and vertically centres the current match. SetYOffset clamps, so the
// centring math never scrolls past either end.
func (m *Model) refreshPreviewSearch() {
	m.previewViewport.SetContent(renderPreviewSearchContent(m.previewLines, m.previewPlain, m.previewMatches, m.previewMatchIdx))
	if len(m.previewMatches) > 0 {
		m.previewViewport.SetYOffset(m.previewMatches[m.previewMatchIdx].line - m.previewViewport.Height/2)
	}
}

// acceptPreviewSearch closes the prompt keeping the query active, so n/N can
// step through the matches. Accepting a blank query just clears the search.
func (m *Model) acceptPreviewSearch() {
	m.previewSearching = false
	m.previewSearchInput.Blur()
	if strings.TrimSpace(m.previewSearchQuery) == "" {
		m.clearPreviewSearch()
	}
}

// clearPreviewSearch removes the active query and its highlights, restoring the
// original preview content. The scroll position is kept so clearing the search
// doesn't lose the user's place.
func (m *Model) clearPreviewSearch() {
	m.previewSearching = false
	m.previewSearchInput.Blur()
	m.previewSearchQuery = ""
	m.previewMatches = nil
	m.previewMatchIdx = 0
	if len(m.previewLines) > 0 {
		m.previewViewport.SetContent(strings.Join(m.previewLines, "\n"))
	}
}

// previewSearchPromptLine renders the inline search prompt with its live match
// count, shown in place of the preview's hint line while the query is typed.
func (m *Model) previewSearchPromptLine() string {
	line := m.previewSearchInput.View() + ui.MutedStyle().Render("   Enter accept · Esc cancel · ↑/↓ prev/next")
	if count := m.previewSearchCount(); count != "" {
		line += "   " + count
	}
	return line
}

// previewSearchCount formats the "match k/N" indicator for the active query,
// "no matches" (in the error colour) when the query matches nothing, and "" for
// a blank query.
func (m *Model) previewSearchCount() string {
	if strings.TrimSpace(m.previewSearchQuery) == "" {
		return ""
	}
	if len(m.previewMatches) == 0 {
		return ui.ErrorStyle().Render("no matches")
	}
	return ui.MutedStyle().Render(fmt.Sprintf("match %d/%d", m.previewMatchIdx+1, len(m.previewMatches)))
}
