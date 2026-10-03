package s3tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// numbered builds a preview whose every line names its own index, so an
// assertion can say exactly which line the cursor landed on.
func numbered(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%02d", i)
	}
	return strings.Join(lines, "\n")
}

// cursorLineText returns the plain text of the line the cursor is drawn on,
// found by rendering the same content with and without the cursor and taking
// the one line that differs. That keeps the assertion on observable output —
// the cursor must actually change something, and exactly one line — without
// sniffing for particular escape codes.
func cursorLineText(t *testing.T, m *Model) string {
	t.Helper()
	w := max(1, m.previewViewport.Width()-previewGutterWidth)
	render := func(cursor int) []string {
		return strings.Split(renderPreviewContent(m.previewLines, m.previewPlain,
			m.previewSearchTerm, m.previewMatches, m.previewMatchIdx, cursor, w), "\n")
	}
	with, without := render(m.previewCursor), render(-1)

	var differ []int
	for i := range with {
		if with[i] != without[i] {
			differ = append(differ, i)
		}
	}
	if len(differ) != 1 {
		t.Fatalf("cursor changed %d lines, want exactly 1 (indices %v)", len(differ), differ)
	}
	if differ[0] != m.previewCursor {
		t.Fatalf("cursor drawn on line %d but previewCursor is %d", differ[0], m.previewCursor)
	}
	// Drop the two-column gutter (and the "▸" match marker when it is there).
	text := strings.TrimSpace(ansi.Strip(with[differ[0]]))
	return strings.TrimSpace(strings.TrimPrefix(text, "▸"))
}

// A fresh preview starts on the first line, and ↑/↓ walk one line at a time.
func TestPreviewCursorStartsAtTopAndSteps(t *testing.T) {
	m := previewModel(t, numbered(50))

	if m.previewCursor != 0 {
		t.Fatalf("cursor starts at %d, want 0", m.previewCursor)
	}
	if got := cursorLineText(t, m); got != "line-00" {
		t.Errorf("cursor line = %q, want line-00", got)
	}

	for range 3 {
		m.handlePreviewCursorKey("down")
	}
	if m.previewCursor != 3 {
		t.Errorf("after 3× down cursor = %d, want 3", m.previewCursor)
	}
	if got := cursorLineText(t, m); got != "line-03" {
		t.Errorf("cursor line = %q, want line-03", got)
	}

	m.handlePreviewCursorKey("up")
	if m.previewCursor != 2 {
		t.Errorf("after up cursor = %d, want 2", m.previewCursor)
	}
}

// The cursor cannot walk off either end — a pager that scrolls past its own
// content is just a blank screen.
func TestPreviewCursorClampsAtBothEnds(t *testing.T) {
	m := previewModel(t, numbered(5))

	for range 10 {
		m.handlePreviewCursorKey("up")
	}
	if m.previewCursor != 0 {
		t.Errorf("cursor ran off the top: %d", m.previewCursor)
	}
	for range 50 {
		m.handlePreviewCursorKey("down")
	}
	if want := len(m.previewLines) - 1; m.previewCursor != want {
		t.Errorf("cursor = %d, want last line %d", m.previewCursor, want)
	}
}

// g/G jump to the ends; the viewport follows.
func TestPreviewCursorHomeAndEnd(t *testing.T) {
	m := previewModel(t, numbered(200))

	m.handlePreviewCursorKey("G")
	last := len(m.previewLines) - 1
	if m.previewCursor != last {
		t.Fatalf("G left cursor at %d, want %d", m.previewCursor, last)
	}
	if top := m.previewViewport.YOffset(); m.previewCursor < top {
		t.Errorf("cursor %d is above the window top %d", m.previewCursor, top)
	}

	m.handlePreviewCursorKey("g")
	if m.previewCursor != 0 || m.previewViewport.YOffset() != 0 {
		t.Errorf("g left cursor=%d offset=%d, want 0/0", m.previewCursor, m.previewViewport.YOffset())
	}
}

// Walking off the bottom edge scrolls by one line, not by a page: reading a
// log should not jump the screen out from under you.
func TestPreviewCursorScrollsOneLineAtTheEdge(t *testing.T) {
	m := previewModel(t, numbered(200))
	h := m.previewViewport.Height()

	for range h - 1 { // land on the last visible line
		m.handlePreviewCursorKey("down")
	}
	if off := m.previewViewport.YOffset(); off != 0 {
		t.Fatalf("window scrolled early: offset %d, want 0", off)
	}
	m.handlePreviewCursorKey("down")
	if off := m.previewViewport.YOffset(); off != 1 {
		t.Errorf("edge step scrolled to %d, want 1", off)
	}
	if m.previewCursor != h {
		t.Errorf("cursor = %d, want %d", m.previewCursor, h)
	}
}

// A key the cursor does not own still reaches the viewport, and the cursor is
// pulled back into view rather than left stranded off-screen.
func TestPreviewCursorFollowsAnExternalScroll(t *testing.T) {
	m := previewModel(t, numbered(200))

	m.previewViewport.SetYOffset(80)
	m.syncPreviewCursorToView()
	if m.previewCursor != 80 {
		t.Errorf("cursor = %d, want pulled to the window top 80", m.previewCursor)
	}

	m.previewViewport.SetYOffset(0)
	m.syncPreviewCursorToView()
	if want := m.previewViewport.Height() - 1; m.previewCursor != want {
		t.Errorf("cursor = %d, want pulled to the window bottom %d", m.previewCursor, want)
	}
}

// The grep filter rebuilds the display lines; a cursor past the new end must
// come back inside rather than index off the slice.
func TestPreviewCursorSurvivesGrepFilter(t *testing.T) {
	m := previewModel(t, numbered(100))

	m.handlePreviewCursorKey("G")
	m.setPreviewGrep("line-0[0-2]") // leaves three lines
	if m.previewCursor >= len(m.previewLines) {
		t.Fatalf("cursor %d is past the %d filtered lines", m.previewCursor, len(m.previewLines))
	}
	if got := cursorLineText(t, m); !strings.HasPrefix(got, "line-0") {
		t.Errorf("cursor line = %q, want one of the filtered lines", got)
	}

	m.setPreviewGrep("") // clearing restores everything
	if m.previewCursor >= len(m.previewLines) {
		t.Errorf("cursor %d is past the %d restored lines", m.previewCursor, len(m.previewLines))
	}
}

// Jumping to a search match leaves the cursor on it, so ↓ carries on from the
// match rather than from wherever the cursor was before.
func TestPreviewCursorLandsOnSearchMatch(t *testing.T) {
	m := previewModel(t, numbered(100))

	m.setPreviewSearchTerm("line-42")
	m.acceptPreviewSearch()
	if len(m.previewMatches) == 0 {
		t.Fatal("no matches for line-42")
	}
	if want := m.previewMatches[m.previewMatchIdx]; m.previewCursor != want {
		t.Errorf("cursor = %d, want the match line %d", m.previewCursor, want)
	}
	if got := cursorLineText(t, m); got != "line-42" {
		t.Errorf("cursor line = %q, want line-42", got)
	}
}

// The bar spans the body width so it reads as a row, and the text survives it
// intact.
func TestPreviewCursorLineIsPaddedAndKeepsItsText(t *testing.T) {
	out := previewCursorLine("short", "", 20)
	if got := ansi.Strip(out); got != "short"+strings.Repeat(" ", 15) {
		t.Errorf("padding = %q", got)
	}
	// A line already at or over the width is not truncated or re-wrapped.
	long := strings.Repeat("x", 30)
	if got := ansi.Strip(previewCursorLine(long, "", 20)); got != long {
		t.Errorf("over-width line changed: %q", got)
	}
	// A search term inside the cursor line keeps its text.
	if got := ansi.Strip(previewCursorLine("find me here", "me", 20)); !strings.HasPrefix(got, "find me here") {
		t.Errorf("term styling lost text: %q", got)
	}
}

// An empty preview has no cursor to move and must not panic or claim a line.
func TestPreviewCursorNoContent(t *testing.T) {
	m := previewModel(t, "")
	if m.handlePreviewCursorKey("down") {
		t.Error("consumed a key with no content")
	}
	m.syncPreviewCursorToView()
	m.clampPreviewCursor()
	if m.previewCursor != 0 {
		t.Errorf("cursor = %d, want 0", m.previewCursor)
	}
}
