package s3tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// "&" opens the grep input, the pattern live-applies while typed, Enter keeps
// the filter, and only matching lines render — mirroring the log viewer.
func TestPreviewGrepFiltersLines(t *testing.T) {
	m := previewModel(t, "INFO start\nERROR boom\nINFO done\nERROR again")

	m.Update(keyRunes("&"))
	if !m.previewGrepActive {
		t.Fatal("& should activate the grep input")
	}
	for _, r := range "ERROR" {
		m.Update(keyRunes(string(r)))
	}
	if m.previewGrepKept != 2 || m.previewGrepTotal != 4 {
		t.Fatalf("kept/total = %d/%d, want 2/4", m.previewGrepKept, m.previewGrepTotal)
	}
	for i, plain := range m.previewPlain {
		if !strings.Contains(plain, "ERROR") {
			t.Errorf("line %d survived the filter: %q", i, plain)
		}
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.previewGrepActive || m.previewGrepRe == nil {
		t.Errorf("enter should keep the filter applied: active=%v re=%v", m.previewGrepActive, m.previewGrepRe)
	}
	if !m.showPreview {
		t.Error("the preview must stay open")
	}
}

// Esc in the grep input clears the filter and restores the full content.
func TestPreviewGrepEscClears(t *testing.T) {
	m := previewModel(t, "alpha\nbeta\ngamma")
	m.Update(keyRunes("&"))
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	if len(m.previewPlain) != 1 {
		t.Fatalf("filtered lines = %d, want 1", len(m.previewPlain))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.previewGrepActive || m.previewGrepRe != nil || m.previewGrepInput.Value() != "" {
		t.Errorf("esc should clear the filter: active=%v re=%v input=%q",
			m.previewGrepActive, m.previewGrepRe, m.previewGrepInput.Value())
	}
	if len(m.previewPlain) != 3 {
		t.Errorf("full content not restored: %d lines, want 3", len(m.previewPlain))
	}
	if !m.showPreview {
		t.Error("esc in the grep input must not close the preview")
	}
}

// The filter uses smart case, like the "/" search on the same page: an
// all-lowercase pattern keeps ERROR/Error/error lines alike, while an
// uppercase letter makes the match exact.
func TestPreviewGrepSmartCase(t *testing.T) {
	m := previewModel(t, "ERROR one\nError two\nerror three\nok")
	m.Update(keyRunes("&"))
	for _, r := range "error" {
		m.Update(keyRunes(string(r)))
	}
	if m.previewGrepKept != 3 {
		t.Fatalf("lowercase pattern kept %d of %d lines, want 3 (case-insensitive): %q",
			m.previewGrepKept, m.previewGrepTotal, m.previewPlain)
	}

	m.previewGrepInput.SetValue("")
	for _, r := range "Error" {
		m.Update(keyRunes(string(r)))
	}
	if m.previewGrepKept != 1 || !strings.Contains(m.previewPlain[0], "Error two") {
		t.Errorf("uppercase pattern kept %d lines (%q), want just \"Error two\"",
			m.previewGrepKept, m.previewPlain)
	}
}

// An invalid in-progress regex is flagged but keeps the last valid filter, so
// a half-typed pattern never blanks the screen.
func TestPreviewGrepInvalidRegexKeepsLastFilter(t *testing.T) {
	m := previewModel(t, "aa\nbb\nab")
	m.Update(keyRunes("&"))
	m.Update(keyRunes("a"))
	if m.previewGrepErr != "" || m.previewGrepKept != 2 {
		t.Fatalf("valid pattern: err=%q kept=%d, want no error and 2", m.previewGrepErr, m.previewGrepKept)
	}
	m.Update(keyRunes("(")) // "a(" does not compile
	if m.previewGrepErr != "invalid regex" {
		t.Errorf("invalid pattern not flagged: err=%q", m.previewGrepErr)
	}
	if m.previewGrepRe == nil || m.previewGrepPat != "a" || m.previewGrepKept != 2 {
		t.Errorf("last valid filter lost: pat=%q kept=%d", m.previewGrepPat, m.previewGrepKept)
	}
}

// The filter drops whole logical lines before wrapping, so a long matching
// line keeps all its wrapped continuations.
func TestPreviewGrepKeepsWrappedContinuations(t *testing.T) {
	long := "needle " + strings.Repeat("x", 300)
	m := previewModel(t, "short filler\n"+long+"\nmore filler")
	m.Update(keyRunes("&"))
	for _, r := range "needle" {
		m.Update(keyRunes(string(r)))
	}
	if m.previewGrepKept != 1 {
		t.Fatalf("kept = %d, want 1 logical line", m.previewGrepKept)
	}
	if len(m.previewPlain) < 2 {
		t.Errorf("wrapped continuations lost: %d display lines", len(m.previewPlain))
	}
	joined := strings.Join(m.previewPlain, "")
	if !strings.Contains(joined, strings.Repeat("x", 300)) {
		t.Error("continuation content missing from the filtered view")
	}
}

// A filter that matches nothing says so instead of showing an empty pane.
func TestPreviewGrepNoMatchNotice(t *testing.T) {
	m := previewModel(t, "alpha\nbeta")
	m.Update(keyRunes("&"))
	for _, r := range "zz" {
		m.Update(keyRunes(string(r)))
	}
	if m.previewGrepKept != 0 {
		t.Fatalf("kept = %d, want 0", m.previewGrepKept)
	}
	if got := ansi.Strip(m.previewViewport.View()); !strings.Contains(got, "No lines match") {
		t.Errorf("empty filter result not explained: %q", got)
	}
}

// The "/" search operates on the filtered lines: matches outside the filter
// disappear and the Find count follows.
func TestPreviewSearchTracksGrepFilter(t *testing.T) {
	m := previewModel(t, "keep hello\ndrop hello\nkeep bye")

	m.Update(keyRunes("/"))
	for _, r := range "hello" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(m.previewMatches) != 2 {
		t.Fatalf("unfiltered matches = %d, want 2", len(m.previewMatches))
	}

	m.Update(keyRunes("&"))
	for _, r := range "keep" {
		m.Update(keyRunes(string(r)))
	}
	if len(m.previewMatches) != 1 {
		t.Errorf("filtered matches = %d, want 1", len(m.previewMatches))
	}
}

// The grep bar takes one viewport line only while visible, and gives it back
// when cleared.
func TestPreviewGrepBarHeightAccounting(t *testing.T) {
	m := previewModel(t, "alpha\nbeta")
	base := m.previewViewport.Height
	m.Update(keyRunes("&"))
	if got := m.previewViewport.Height; got != base-1 {
		t.Errorf("viewport height with grep bar = %d, want %d", got, base-1)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if got := m.previewViewport.Height; got != base {
		t.Errorf("viewport height after clearing = %d, want %d", got, base)
	}
}

// A new preview starts with the filter fully reset.
func TestPreviewGrepResetOnNewPreview(t *testing.T) {
	m := previewModel(t, "alpha\nbeta")
	m.Update(keyRunes("&"))
	for _, r := range "beta" {
		m.Update(keyRunes(string(r)))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m.initPreviewViewport("other content", nil)
	if m.previewGrepRe != nil || m.previewGrepActive || m.previewGrepInput.Value() != "" {
		t.Errorf("grep state leaked into a new preview: re=%v active=%v input=%q",
			m.previewGrepRe, m.previewGrepActive, m.previewGrepInput.Value())
	}
	if len(m.previewPlain) != 1 || m.previewPlain[0] != "other content" {
		t.Errorf("new preview lines = %v", m.previewPlain)
	}
}
