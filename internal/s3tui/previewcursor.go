package s3tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// Line cursor in the object preview.
//
// The preview used to be a bare scrolling viewport: ↑/↓ moved the window and
// nothing said which line you were on. Most of what gets previewed out of a
// bucket is a log, where that matters — a wrapped event is hard to follow
// across its continuation lines, and there is nothing to keep your place
// against while reading. The CloudWatch log viewer has always had a cursor
// (it is a table), so this gives the preview the same reading model, in the
// shared table's selected-row colours so the two pages look like one tool
// (CLAUDE.md §16).
//
// The cursor indexes the *display* lines — after wrapping and after the "&"
// grep filter — the same index space the "/" search matches use, so the two
// features always agree about what a line is.

// movePreviewCursor moves the cursor by delta lines, clamped to the content.
func (m *Model) movePreviewCursor(delta int) { m.setPreviewCursor(m.previewCursor + delta) }

// setPreviewCursor puts the cursor on line i (clamped), scrolls it into view
// and re-renders.
func (m *Model) setPreviewCursor(i int) {
	n := len(m.previewLines)
	if n == 0 {
		m.previewCursor = 0
		return
	}
	m.previewCursor = min(max(i, 0), n-1)
	m.scrollPreviewToCursor()
	m.refreshPreviewContent()
}

// scrollPreviewToCursor scrolls the smallest amount that brings the cursor
// back into view, so reading down a file advances the window one line at a
// time instead of jumping a page whenever the cursor reaches the edge.
func (m *Model) scrollPreviewToCursor() {
	h := m.previewViewport.Height()
	if h <= 0 {
		return
	}
	switch top := m.previewViewport.YOffset(); {
	case m.previewCursor < top:
		m.previewViewport.SetYOffset(m.previewCursor)
	case m.previewCursor > top+h-1:
		m.previewViewport.SetYOffset(m.previewCursor - h + 1)
	}
}

// syncPreviewCursorToView pulls the cursor back into the visible window after
// something other than the cursor moved it — a mouse wheel, or a key the
// viewport handled itself. Without this the highlight would sit off-screen and
// the next ↑/↓ would appear to teleport.
func (m *Model) syncPreviewCursorToView() {
	n := len(m.previewLines)
	if n == 0 {
		return
	}
	h := max(1, m.previewViewport.Height())
	top := max(0, m.previewViewport.YOffset())
	bottom := min(top+h-1, n-1)
	switch {
	case m.previewCursor < top:
		m.previewCursor = min(top, n-1)
	case m.previewCursor > bottom:
		m.previewCursor = max(bottom, 0)
	default:
		return // already visible — don't re-render for nothing
	}
	m.refreshPreviewContent()
}

// clampPreviewCursor keeps the cursor inside the content after the display
// lines are rebuilt (a grep filter applied or cleared, a resize re-wrapping
// the text). It does not re-render: rebuildPreviewLines does that itself.
func (m *Model) clampPreviewCursor() {
	if n := len(m.previewLines); m.previewCursor >= n {
		m.previewCursor = max(0, n-1)
	}
}

// handlePreviewCursorKey moves the cursor for a navigation key, reporting
// whether it consumed the key.
//
// The key set mirrors the viewport's own pager bindings, so no key a user
// already presses changes meaning — only what it moves: the cursor leads and
// the window follows. g/G are added because the viewport binds neither.
func (m *Model) handlePreviewCursorKey(key string) bool {
	if len(m.previewLines) == 0 {
		return false
	}
	page := max(1, m.previewViewport.Height())
	half := max(1, page/2)

	switch key {
	case "up", "k":
		m.movePreviewCursor(-1)
	case "down", "j":
		m.movePreviewCursor(1)
	case "pgup", "b":
		m.movePreviewCursor(-page)
	case "pgdown", "f", "space", " ":
		m.movePreviewCursor(page)
	case "u", "ctrl+u":
		m.movePreviewCursor(-half)
	case "d", "ctrl+d":
		m.movePreviewCursor(half)
	case "home", "g":
		m.setPreviewCursor(0)
	case "end", "G":
		m.setPreviewCursor(len(m.previewLines) - 1)
	default:
		return false
	}
	return true
}

// previewCursorLine renders one line as the cursor line: its plain text on the
// shared table's selected-row colours, padded to the full body width so the
// highlight reads as a whole row rather than a ragged one.
//
// It is built from the plain text rather than the syntax-coloured line for the
// same reason the search highlight is: a background wrapped around a string
// that carries its own SGR resets would stop at the first reset. Every segment
// — including the padding, and any search term inside the line — therefore
// carries the background itself, so the bar is continuous.
func previewCursorLine(plain, term string, width int) string {
	base := lipgloss.NewStyle().
		Background(lipgloss.Color(ui.ColorTableSelectedBg())).
		Foreground(lipgloss.Color(ui.ColorTableSelectedText()))
	// The search's own highlight colours would be invisible against the
	// cursor bar, so a match on the cursor line is marked by weight instead.
	hit := base.Bold(true).Underline(true)

	var b strings.Builder
	write := func(style lipgloss.Style, s string) {
		if s != "" {
			b.WriteString(style.Render(s))
		}
	}
	pos := 0
	for _, s := range termSpans(plain, term) {
		write(base, plain[pos:s[0]])
		write(hit, plain[s[0]:s[1]])
		pos = s[1]
	}
	write(base, plain[pos:])
	write(base, strings.Repeat(" ", max(0, width-ansi.StringWidth(plain))))
	return b.String()
}
