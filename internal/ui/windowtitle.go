package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// Titled is a tea.Model that can name the screen it is currently showing.
type Titled interface {
	tea.Model
	PageTitle() string
}

// settingsTarget is where the Appearance panel saves (see ConfigureSettings).
var settingsTarget struct {
	path string
	cfg  *config.Config
}

// ConfigureSettings tells every TUI where the Appearance panel (ctrl+t) saves:
// the config file path and the loaded config it rewrites. Called once at
// start-up; until it is, ctrl+t does nothing and no hint is shown.
func ConfigureSettings(path string, cfg *config.Config) {
	settingsTarget.path, settingsTarget.cfg = path, cfg
}

func settingsConfigured() bool { return settingsTarget.cfg != nil }

// WithWindowTitle wraps a TUI's model in the application shell every program
// is built around. The shell:
//
//   - keeps the terminal window/tab title naming the screen being shown (e.g.
//     "VPC Explorer › my-vpc › Subnets") for models that implement Titled, so
//     "which screen are you on?" is always answerable;
//   - opens the Appearance panel on ctrl+t (KeyAppearance) over any screen of
//     any TUI — theme, icons and painted background, applied live, Ctrl+S to
//     save — without each TUI having to integrate it;
//   - paints the finished frame's background when ui.paintBackground is on
//     (see Paint).
func WithWindowTitle(m tea.Model) tea.Model {
	return &shellModel{inner: m}
}

type shellModel struct {
	inner tea.Model
	last  string // last window title sent
	size  tea.WindowSizeMsg

	settingsOpen bool
	settings     SettingsModel

	toast string
}

type shellToastDoneMsg struct{}

func (s *shellModel) Init() tea.Cmd {
	cmd := s.inner.Init()
	if t, ok := s.inner.(Titled); ok {
		s.last = t.PageTitle()
		cmd = tea.Batch(cmd, tea.SetWindowTitle(s.last))
	}
	return cmd
}

func (s *shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.size = m
		if s.settingsOpen {
			s.settings, _ = s.settings.Update(m)
		}
	case tea.KeyMsg:
		// While the panel is open it owns the keyboard; every other message
		// still reaches the TUI below, so scans and streams keep running (§10).
		if s.settingsOpen {
			switch k := m.String(); {
			case k == "ctrl+c":
				return s, tea.Quit
			case (k == "esc" || k == KeyAppearance) && !s.settings.EditMode():
				s.settingsOpen = false
				return s, nil
			}
			var cmd tea.Cmd
			s.settings, cmd = s.settings.Update(m)
			return s, cmd
		}
		if m.String() == KeyAppearance && settingsConfigured() {
			s.settings = NewSettingsModel(s.size.Width, s.size.Height, settingsTarget.path, settingsTarget.cfg)
			s.settingsOpen = true
			return s, nil
		}
	case tea.MouseMsg:
		if s.settingsOpen {
			return s, nil
		}
	case SettingsSavedMsg:
		if s.settingsOpen {
			s.settingsOpen = false
			return s, s.showToast("Appearance saved · theme " + m.Theme)
		}
	case SettingsErrMsg:
		if s.settingsOpen {
			s.settingsOpen = false
			return s, s.showToast("Could not save appearance: " + m.Err.Error())
		}
	case shellToastDoneMsg:
		s.toast = ""
		return s, nil
	}

	mm, cmd := s.inner.Update(msg)
	s.inner = mm
	if t, ok := mm.(Titled); ok {
		if title := t.PageTitle(); title != s.last {
			s.last = title
			cmd = tea.Batch(cmd, tea.SetWindowTitle(title))
		}
	}
	return s, cmd
}

func (s *shellModel) showToast(text string) tea.Cmd {
	s.toast = text
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return shellToastDoneMsg{} })
}

func (s *shellModel) View() string {
	frame := s.inner.View()
	w, h := s.size.Width, s.size.Height
	if s.settingsOpen && w > 0 && h > 0 {
		// The panel floats over the live screen, so theme changes show on the
		// real UI around it.
		frame = OverlayCenter(frame, s.settings.View(), w, h)
	}
	if s.toast != "" && w > 0 {
		toast := lipgloss.NewStyle().
			Background(lipgloss.Color(ColorSuccess())).
			Foreground(lipgloss.Color(ColorHighlightText())).
			Padding(0, 2).Bold(true).Render(s.toast)
		lines := strings.Split(frame, "\n")
		lines[0] = lipgloss.PlaceHorizontal(w, lipgloss.Right, toast)
		frame = strings.Join(lines, "\n")
	}
	return Paint(frame, w, h)
}
