package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeTitled struct{ title string }

func (f fakeTitled) Init() tea.Cmd { return nil }
func (f fakeTitled) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s, ok := msg.(string); ok {
		f.title = s
	}
	return f, nil
}
func (f fakeTitled) View() string      { return "" }
func (f fakeTitled) PageTitle() string { return f.title }

type untitled struct{}

func (u untitled) Init() tea.Cmd                       { return nil }
func (u untitled) Update(tea.Msg) (tea.Model, tea.Cmd) { return u, nil }
func (u untitled) View() string                        { return "" }

func TestWithWindowTitleSyncsOnChange(t *testing.T) {
	m := WithWindowTitle(fakeTitled{title: "App › Home"})
	w, ok := m.(*titledModel)
	if !ok {
		t.Fatal("expected a titled wrapper for a Titled model")
	}
	if cmd := w.Init(); cmd == nil {
		t.Fatal("Init must set the initial window title")
	}
	if w.last != "App › Home" {
		t.Fatalf("initial title = %q", w.last)
	}

	// A message that changes the page must emit a title command.
	mm, cmd := w.Update("App › Detail")
	if cmd == nil {
		t.Fatal("expected a SetWindowTitle command on page change")
	}
	w = mm.(*titledModel)
	if w.last != "App › Detail" {
		t.Fatalf("title not tracked, got %q", w.last)
	}

	// No page change → no extra command.
	if _, cmd := w.Update(struct{}{}); cmd != nil {
		t.Fatal("expected no command when the title is unchanged")
	}
}

// An untitled model gets no title syncing, only the background painting —
// which, off by default, leaves its frame exactly as it rendered it.
func TestWithWindowTitleUntitledIsPaintOnly(t *testing.T) {
	SetPaintBackground(false)
	m := untitled{}
	got := WithWindowTitle(m)
	if _, ok := got.(*paintedModel); !ok {
		t.Fatalf("untitled model should be wrapped for painting, got %T", got)
	}
	if got.View() != m.View() {
		t.Error("with painting off the frame must be unchanged")
	}
	if _, cmd := got.Update(struct{}{}); cmd != nil {
		t.Error("an untitled model must not get window-title commands")
	}
}
