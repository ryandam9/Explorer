package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
func (f fakeTitled) View() tea.View    { return tea.NewView("") }
func (f fakeTitled) PageTitle() string { return f.title }

type untitled struct{}

func (u untitled) Init() tea.Cmd                       { return nil }
func (u untitled) Update(tea.Msg) (tea.Model, tea.Cmd) { return u, nil }
func (u untitled) View() tea.View                      { return tea.NewView("frame") }

// Bubble Tea v2 takes the window title and terminal modes from the View: the
// shell declares the page title, the alternate screen, and mouse reporting
// only for programs that asked for it.
func TestShellDeclaresTitleAndModes(t *testing.T) {
	m := WithWindowTitle(fakeTitled{title: "App › Home"})
	v := m.View()
	if v.WindowTitle != "App › Home" || !v.AltScreen || v.MouseMode != tea.MouseModeNone {
		t.Fatalf("view = title %q alt %v mouse %v", v.WindowTitle, v.AltScreen, v.MouseMode)
	}
	mm, _ := m.Update("App › Detail")
	if got := mm.View().WindowTitle; got != "App › Detail" {
		t.Fatalf("title should follow the page, got %q", got)
	}

	if v := WithWindowTitle(untitled{}, WithMouse()).View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Error("WithMouse should turn on cell-motion mouse reporting")
	}
}

// An untitled model gets no window title — and, with painting off, its frame
// is exactly what it rendered.
func TestWithWindowTitleUntitled(t *testing.T) {
	SetPaintBackground(false)
	got := WithWindowTitle(untitled{}).View()
	if got.Content != "frame" || got.WindowTitle != "" {
		t.Errorf("untitled view = %+v", got)
	}
}

// recorder counts the messages that reach the TUI under the shell.
type recorder struct {
	keys, other int
}

func (r *recorder) Init() tea.Cmd { return nil }
func (r *recorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		r.keys++
	} else {
		r.other++
	}
	return r, nil
}
func (r *recorder) View() tea.View { return tea.NewView("screen") }

func ctrlT() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl} }

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
	if !strings.Contains(sh.View().Content, "Appearance") {
		t.Error("the panel should float over the screen")
	}
	keys, other := r.keys, r.other
	sh.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	sh.Update(struct{}{})
	if r.keys != keys {
		t.Error("keys must go to the panel while it is open")
	}
	if r.other != other+1 {
		t.Error("non-key messages must still reach the TUI while the panel is open")
	}
	sh.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
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
