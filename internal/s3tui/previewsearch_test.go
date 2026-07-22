package s3tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// previewModel builds a Model showing content in the text preview overlay, the
// way objectPreviewMsg would after a "p".
func previewModel(t *testing.T, content string) *Model {
	t.Helper()
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
		showPreview: true, previewKey: "notes.txt", previewContent: content}
	m.previewSearchInput = textinput.New()
	m.initPreviewViewport(content, nil)
	return m
}

func TestFindPreviewMatchesCaseInsensitiveAndOrdered(t *testing.T) {
	plain := []string{"Error: retry", "ok", "error ERROR"}
	ms := findPreviewMatches(plain, "error")
	want := []previewMatch{{line: 0, start: 0, end: 5}, {line: 2, start: 0, end: 5}, {line: 2, start: 6, end: 11}}
	if len(ms) != len(want) {
		t.Fatalf("matches = %v, want %v", ms, want)
	}
	for i := range want {
		if ms[i] != want[i] {
			t.Errorf("match[%d] = %v, want %v", i, ms[i], want[i])
		}
	}
	if got := findPreviewMatches(plain, "  "); got != nil {
		t.Errorf("blank query matched: %v", got)
	}
	if got := findPreviewMatches(plain, "absent"); got != nil {
		t.Errorf("non-occurring query matched: %v", got)
	}
}

// A matched line is re-rendered from plain text with the matches styled; its
// stripped form must equal the plain line (nothing lost or duplicated), and
// unmatched lines keep their original (possibly styled) form.
func TestRenderPreviewSearchContentPreservesText(t *testing.T) {
	lines := []string{"\x1b[32mhello world\x1b[0m", "plain hello"}
	plain := []string{"hello world", "plain hello"}
	ms := findPreviewMatches(plain, "hello")

	out := strings.Split(renderPreviewSearchContent(lines, plain, ms, 0), "\n")
	if len(out) != 2 {
		t.Fatalf("rendered %d lines, want 2", len(out))
	}
	for i, line := range out {
		if got := ansi.Strip(line); got != plain[i] {
			t.Errorf("line %d stripped = %q, want %q", i, got, plain[i])
		}
	}
	// A matched styled line is rebuilt from its plain text (the highlight
	// offsets index plain text, so the old ANSI must be gone).
	if strings.Contains(out[0], "\x1b[32m") {
		t.Errorf("matched line kept its original ANSI styling: %q", out[0])
	}

	// With no matches the original styled content is returned untouched.
	if got := renderPreviewSearchContent(lines, plain, nil, 0); got != strings.Join(lines, "\n") {
		t.Errorf("no-match render altered content: %q", got)
	}
}

// "/" opens the prompt, typing matches live, Enter keeps the query for n/N,
// and Esc clears the search before a second Esc closes the preview.
func TestPreviewSearchKeyFlow(t *testing.T) {
	m := previewModel(t, "alpha\nbeta\nalpha beta\ngamma")

	m.Update(keyRunes("/"))
	if !m.previewSearching {
		t.Fatal("/ should open the preview search prompt")
	}
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	if len(m.previewMatches) != 2 {
		t.Fatalf("live matches = %d, want 2", len(m.previewMatches))
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.previewSearching || m.previewSearchQuery != "beta" {
		t.Fatalf("enter should accept the query: searching=%v query=%q", m.previewSearching, m.previewSearchQuery)
	}

	m.Update(keyRunes("n"))
	if m.previewMatchIdx != 1 {
		t.Errorf("n should advance to match 2, idx=%d", m.previewMatchIdx)
	}
	m.Update(keyRunes("n")) // wraps
	if m.previewMatchIdx != 0 {
		t.Errorf("n should wrap to the first match, idx=%d", m.previewMatchIdx)
	}
	m.Update(keyRunes("N")) // wraps backward
	if m.previewMatchIdx != 1 {
		t.Errorf("N should wrap to the last match, idx=%d", m.previewMatchIdx)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.previewSearchQuery != "" || m.previewMatches != nil {
		t.Fatalf("first esc should clear the search, query=%q", m.previewSearchQuery)
	}
	if !m.showPreview {
		t.Fatal("first esc must not close the preview")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showPreview {
		t.Error("second esc should close the preview")
	}
}

// While typing, global keys like "q" are text, not quit.
func TestPreviewSearchTypingCapturesGlobalKeys(t *testing.T) {
	m := previewModel(t, "quick brown fox")
	m.Update(keyRunes("/"))
	m.Update(keyRunes("q"))
	if got := m.previewSearchInput.Value(); got != "q" {
		t.Errorf("input value = %q, want %q", got, "q")
	}
	if !m.showPreview || !m.previewSearching {
		t.Error("preview and prompt should stay open while typing")
	}
}

// Stepping to a match beyond the viewport scrolls it into view.
func TestPreviewSearchScrollsToMatch(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		b.WriteString("filler line\n")
	}
	b.WriteString("needle at the bottom")
	m := previewModel(t, b.String())

	m.Update(keyRunes("/"))
	for _, r := range "needle" {
		m.Update(keyRunes(string(r)))
	}
	if len(m.previewMatches) != 1 {
		t.Fatalf("matches = %d, want 1", len(m.previewMatches))
	}
	if m.previewViewport.YOffset == 0 {
		t.Error("viewport should scroll so the match is visible")
	}
}

// A new preview starts clean: no active query or highlights carried over.
func TestPreviewSearchResetOnNewPreview(t *testing.T) {
	m := previewModel(t, "alpha beta")
	m.Update(keyRunes("/"))
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m.initPreviewViewport("other content", nil)
	if m.previewSearchQuery != "" || m.previewMatches != nil || m.previewSearching {
		t.Errorf("search state leaked into a new preview: query=%q matches=%v", m.previewSearchQuery, m.previewMatches)
	}
}
