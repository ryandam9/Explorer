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
	m.previewGrepInput = textinput.New()
	m.initPreviewViewport(content, nil)
	return m
}

func TestComputePreviewMatchesCaseInsensitiveLineIndices(t *testing.T) {
	plain := []string{"Error: retry", "ok", "error ERROR", "fine"}
	if got := computePreviewMatches(plain, "error"); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("matches = %v, want [0 2]", got)
	}
	if got := computePreviewMatches(plain, ""); got != nil {
		t.Errorf("empty term matched: %v", got)
	}
	if got := computePreviewMatches(plain, "absent"); got != nil {
		t.Errorf("non-occurring term matched: %v", got)
	}
}

func TestTermSpansFindsAllOccurrences(t *testing.T) {
	spans := termSpans("error ERROR err", "error")
	want := [][2]int{{0, 5}, {6, 11}}
	if len(spans) != len(want) {
		t.Fatalf("spans = %v, want %v", spans, want)
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span[%d] = %v, want %v", i, spans[i], want[i])
		}
	}
}

// Every rendered line carries the two-column gutter; the current match line is
// marked "▸"; matched lines are rebuilt from plain text so the highlight can
// never drift inside pre-existing ANSI; and nothing is lost or duplicated.
func TestRenderPreviewContentGutterAndText(t *testing.T) {
	lines := []string{"\x1b[32mhello world\x1b[0m", "no match here", "plain hello"}
	plain := []string{"hello world", "no match here", "plain hello"}
	matches := computePreviewMatches(plain, "hello") // lines 0 and 2

	out := strings.Split(renderPreviewContent(lines, plain, "hello", matches, 1), "\n")
	if len(out) != 3 {
		t.Fatalf("rendered %d lines, want 3", len(out))
	}
	for i, line := range out {
		stripped := ansi.Strip(line)
		wantGutter := "  "
		if i == 2 { // matches[1] == line 2 is the current match
			wantGutter = "▸ "
		}
		if !strings.HasPrefix(stripped, wantGutter) {
			t.Errorf("line %d gutter = %q, want prefix %q", i, stripped, wantGutter)
		}
		if got := strings.TrimPrefix(stripped, wantGutter); got != plain[i] {
			t.Errorf("line %d text = %q, want %q", i, got, plain[i])
		}
	}
	// The matched styled line is rebuilt from its plain text.
	if strings.Contains(out[0], "\x1b[32m") {
		t.Errorf("matched line kept its original ANSI styling: %q", out[0])
	}
	// Without a term no line is marked.
	for i, line := range strings.Split(renderPreviewContent(lines, plain, "", nil, 0), "\n") {
		if !strings.HasPrefix(ansi.Strip(line), "  ") {
			t.Errorf("no-search line %d not gutter-padded: %q", i, ansi.Strip(line))
		}
	}
}

// "/" opens the Find input, typing highlights live without scrolling, Enter
// accepts and jumps, n/N step matching lines with wrap-around, and Esc (in
// the input) clears the search — mirroring the CloudWatch log viewer.
func TestPreviewSearchKeyFlow(t *testing.T) {
	m := previewModel(t, "alpha\nbeta\nalpha beta\ngamma")

	m.Update(keyRunes("/"))
	if !m.previewSearching {
		t.Fatal("/ should activate the Find input")
	}
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	if len(m.previewMatches) != 2 {
		t.Fatalf("live matches = %d, want 2", len(m.previewMatches))
	}
	if m.previewViewport.YOffset != 0 {
		t.Error("typing must not scroll the preview")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.previewSearching || m.previewSearchTerm != "beta" {
		t.Fatalf("enter should accept the term: searching=%v term=%q", m.previewSearching, m.previewSearchTerm)
	}
	if m.previewMatchIdx != 0 {
		t.Errorf("enter should land on the first match from the top, idx=%d", m.previewMatchIdx)
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

	// Esc with an accepted term closes the preview (the term is cleared only
	// via / then Esc, as in the log viewer).
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.showPreview {
		t.Fatal("esc should close the preview")
	}
}

// Esc while the Find input is active clears the whole search but keeps the
// preview open.
func TestPreviewSearchEscInInputClears(t *testing.T) {
	m := previewModel(t, "alpha\nbeta")
	m.Update(keyRunes("/"))
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.previewSearching || m.previewSearchTerm != "" || m.previewMatches != nil {
		t.Errorf("esc in the input should clear the search: searching=%v term=%q matches=%v",
			m.previewSearching, m.previewSearchTerm, m.previewMatches)
	}
	if got := m.previewSearchInput.Value(); got != "" {
		t.Errorf("esc should clear the input text, got %q", got)
	}
	if !m.showPreview {
		t.Error("esc in the input must not close the preview")
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
		t.Error("preview and Find input should stay open while typing")
	}
}

// Enter jumps to the first match at or after the current scroll position, not
// back to the top (the log viewer's jumpToFirstMatchFrom).
func TestPreviewSearchEnterJumpsFromCurrentOffset(t *testing.T) {
	var b strings.Builder
	b.WriteString("needle first\n")
	for i := 0; i < 60; i++ {
		b.WriteString("filler line\n")
	}
	b.WriteString("needle at the bottom")
	m := previewModel(t, b.String())

	// Scroll well past the first match before searching.
	m.previewViewport.SetYOffset(30)

	m.Update(keyRunes("/"))
	for _, r := range "needle" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if len(m.previewMatches) != 2 {
		t.Fatalf("matches = %d, want 2", len(m.previewMatches))
	}
	if m.previewMatchIdx != 1 {
		t.Errorf("enter should pick the match after the scroll position, idx=%d", m.previewMatchIdx)
	}
	if m.previewViewport.YOffset <= 30 {
		t.Errorf("viewport should centre the bottom match, YOffset=%d", m.previewViewport.YOffset)
	}
}

// "?" over the preview opens a context-aware help page that documents the
// search keys; while the Find input is active, "?" is text instead.
func TestPreviewHelpListsSearchKeys(t *testing.T) {
	m := previewModel(t, "some text")

	m.Update(keyRunes("?"))
	if !m.showHelp {
		t.Fatal("? should open the help overlay over the preview")
	}
	help := ansi.Strip(m.helpView())
	for _, want := range []string{"Object Preview", "Find in the previewed text", "n / N", "CloudWatch log page"} {
		if !strings.Contains(help, want) {
			t.Errorf("preview help missing %q", want)
		}
	}
	m.Update(keyRunes("?")) // close help again

	m.Update(keyRunes("/"))
	m.Update(keyRunes("?"))
	if m.showHelp {
		t.Error("? typed into the Find input must not open help")
	}
	if got := m.previewSearchInput.Value(); got != "?" {
		t.Errorf("input value = %q, want %q", got, "?")
	}
}

// A new preview starts clean: no term, matches, or input text carried over.
func TestPreviewSearchResetOnNewPreview(t *testing.T) {
	m := previewModel(t, "alpha beta")
	m.Update(keyRunes("/"))
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m.initPreviewViewport("other content", nil)
	if m.previewSearchTerm != "" || m.previewMatches != nil || m.previewSearching {
		t.Errorf("search state leaked into a new preview: term=%q matches=%v", m.previewSearchTerm, m.previewMatches)
	}
	if got := m.previewSearchInput.Value(); got != "" {
		t.Errorf("input text leaked into a new preview: %q", got)
	}
}
