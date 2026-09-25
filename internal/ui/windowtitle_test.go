package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/config"
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
	w, ok := m.(*shellModel)
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
	w = mm.(*shellModel)
	if w.last != "App › Detail" {
		t.Fatalf("title not tracked, got %q", w.last)
	}

	// No page change → no extra command.
	if _, cmd := w.Update(struct{}{}); cmd != nil {
		t.Fatal("expected no command when the title is unchanged")
	}
}

// An untitled model gets no title syncing — and, with painting off, its frame
// is exactly what it rendered.
func TestWithWindowTitleUntitled(t *testing.T) {
	SetPaintBackground(false)
	m := untitled{}
	got := WithWindowTitle(m)
	if got.View() != m.View() {
		t.Error("with painting off the frame must be unchanged")
	}
	if cmd := got.Init(); cmd != nil {
		t.Error("an untitled model must not get window-title commands")
	}
	if _, cmd := got.Update(struct{}{}); cmd != nil {
		t.Error("an untitled model must not get window-title commands")
	}
}

// recorder counts the messages that reach the TUI under the shell.
type recorder struct {
	keys, other int
}

func (r *recorder) Init() tea.Cmd { return nil }
func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		r.keys++
	} else {
		r.other++
	}
	return r, nil
}
func (r *recorder) View() string { return "screen" }

func ctrlT() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlT} }

// ctrl+t opens the Appearance panel over any TUI once configured. While it is
// open it owns the keyboard, but every other message still reaches the TUI
// (so scans keep streaming); Esc closes it.
func TestShellAppearancePanel(t *testing.T) {
	ConfigureSettings("", nil)
	r := &recorder{}
	sh := WithWindowTitle(r).(*shellModel)
	sh.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	sh.Update(ctrlT())
	if sh.settingsOpen || r.keys != 1 {
		t.Fatal("unconfigured, ctrl+t must pass through to the TUI")
	}

	ConfigureSettings("", &config.Config{})
	defer ConfigureSettings("", nil)
	sh.Update(ctrlT())
	if !sh.settingsOpen {
		t.Fatal("ctrl+t should open the Appearance panel")
	}
	if !strings.Contains(sh.View(), "Appearance") {
		t.Error("the panel should float over the screen")
	}
	keys, other := r.keys, r.other
	sh.Update(tea.KeyMsg{Type: tea.KeyDown})
	sh.Update(struct{}{})
	if r.keys != keys {
		t.Error("keys must go to the panel while it is open")
	}
	if r.other != other+1 {
		t.Error("non-key messages must still reach the TUI while the panel is open")
	}
	sh.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if sh.settingsOpen {
		t.Error("Esc should close the panel")
	}
}

// Every status bar advertises ^T once the panel is configured, before the
// final (always-kept) hint.
func TestStatusBarAdvertisesAppearance(t *testing.T) {
	hints := []KeyHint{H("↑/↓", "rows"), H("q", "quit")}
	ConfigureSettings("", nil)
	if strings.Contains(StatusBar(120, "", hints), "^T") {
		t.Error("no ^T hint until the panel is configured")
	}
	ConfigureSettings("", &config.Config{})
	defer ConfigureSettings("", nil)
	got := withAppHints(hints)
	if len(got) != 3 || got[1].Key != "^T" || got[2].Key != "q" {
		t.Errorf("hints = %v", got)
	}
}
