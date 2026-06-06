package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v3"

	"github.com/user/aws_explorer/internal/config"
)

// SettingsSavedMsg is sent after a successful config save.
type SettingsSavedMsg struct{ Theme string }

// SettingsErrMsg carries a save error back to the main model.
type SettingsErrMsg struct{ Err error }

// settingsField enumerates the editable color roles.
type settingsField int

const (
	sfHeading settingsField = iota
	sfText
	sfBackground
	sfBorder
	sfBorderFocus
	sfHighlight
	sfHighlightText
	sfMuted
	sfTableHeader
	sfTableHeaderLine
	sfStatusBarBg
	sfStatusBarText
	sfAccent
	sfError
	sfWarning
	sfFieldCount
)

var settingsFieldNames = [sfFieldCount]string{
	"Heading", "Text", "Background", "Border", "BorderFocus",
	"Highlight", "HighlightText", "Muted", "TableHeader", "TableHeaderLine",
	"StatusBarBg", "StatusBarText", "Accent", "Error", "Warning",
}

// SettingsModel drives the settings overlay panel.
type SettingsModel struct {
	// Width / height of the terminal.
	width, height int

	// Theme list navigation.
	themeIdx int // index into Themes
	fieldIdx int // which color role is being edited (0-based)
	editMode bool
	editBuf  string // in-progress text while editing

	// Path to the config file — needed for saving.
	configPath string

	// In-memory copy of the full config so we can write it back.
	fullConfig *config.Config
}

func NewSettingsModel(width, height int, configPath string, cfg *config.Config) SettingsModel {
	return SettingsModel{
		width:      width,
		height:     height,
		themeIdx:   getActiveTheme(),
		configPath: configPath,
		fullConfig: cfg,
	}
}

// EditMode reports whether the panel is currently capturing text input for a
// color value. Callers use this to decide whether Esc should close the panel.
func (s SettingsModel) EditMode() bool { return s.editMode }

// colorForField reads the current in-memory color for a role from Themes[].
func (s SettingsModel) colorForField(themeIdx int, f settingsField) string {
	c := Themes[themeIdx].Colors
	switch f {
	case sfHeading:
		return c.Heading
	case sfText:
		return c.Text
	case sfBackground:
		return c.Background
	case sfBorder:
		return c.Border
	case sfBorderFocus:
		return c.BorderFocus
	case sfHighlight:
		return c.Highlight
	case sfHighlightText:
		return c.HighlightText
	case sfMuted:
		return c.Muted
	case sfTableHeader:
		return c.TableHeader
	case sfTableHeaderLine:
		return c.TableHeaderLine
	case sfStatusBarBg:
		return c.StatusBarBg
	case sfStatusBarText:
		return c.StatusBarText
	case sfAccent:
		return c.Accent
	case sfError:
		return c.Error
	case sfWarning:
		return c.Warning
	}
	return ""
}

// setColorForField updates the in-memory Themes[] entry.
func setColorForField(themeIdx int, f settingsField, val string) {
	c := &Themes[themeIdx].Colors
	switch f {
	case sfHeading:
		c.Heading = val
	case sfText:
		c.Text = val
	case sfBackground:
		c.Background = val
	case sfBorder:
		c.Border = val
	case sfBorderFocus:
		c.BorderFocus = val
	case sfHighlight:
		c.Highlight = val
	case sfHighlightText:
		c.HighlightText = val
	case sfMuted:
		c.Muted = val
	case sfTableHeader:
		c.TableHeader = val
	case sfTableHeaderLine:
		c.TableHeaderLine = val
	case sfStatusBarBg:
		c.StatusBarBg = val
	case sfStatusBarText:
		c.StatusBarText = val
	case sfAccent:
		c.Accent = val
	case sfError:
		c.Error = val
	case sfWarning:
		c.Warning = val
	}
}

func (s SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if s.editMode {
			return s.updateEditMode(msg)
		}
		return s.updateNavMode(msg)
	}
	return s, nil
}

func (s SettingsModel) updateNavMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if s.fieldIdx > 0 {
			s.fieldIdx--
		}
	case "down", "j":
		if s.fieldIdx < int(sfFieldCount)-1 {
			s.fieldIdx++
		}
	case "left", "h":
		if s.themeIdx > 0 {
			s.themeIdx--
		}
	case "right", "l":
		if s.themeIdx < len(Themes)-1 {
			s.themeIdx++
		}
	case "enter", "e":
		s.editMode = true
		s.editBuf = s.colorForField(s.themeIdx, settingsField(s.fieldIdx))
	case "ctrl+s", "w":
		return s, s.saveCmd()
	}
	return s, nil
}

func (s SettingsModel) updateEditMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		setColorForField(s.themeIdx, settingsField(s.fieldIdx), strings.TrimSpace(s.editBuf))
		s.editMode = false
	case "esc":
		s.editMode = false
	case "backspace", "ctrl+h":
		if len(s.editBuf) > 0 {
			s.editBuf = s.editBuf[:len(s.editBuf)-1]
		}
	default:
		if len(msg.Runes) > 0 {
			s.editBuf += string(msg.Runes)
		}
	}
	return s, nil
}

// saveCmd persists the current in-memory theme state back to config.yaml.
func (s SettingsModel) saveCmd() tea.Cmd {
	return func() tea.Msg {
		if s.configPath == "" || s.fullConfig == nil {
			return SettingsErrMsg{fmt.Errorf("config path not set")}
		}

		// Build the updated UI config from the in-memory Themes slice.
		uiCfg := config.UIConfig{
			Theme:  Themes[s.themeIdx].Name,
			Themes: make(map[string]config.ThemeColorConfig, len(Themes)),
		}
		for _, t := range Themes {
			uiCfg.Themes[t.Name] = config.ThemeColorConfig{
				Heading:         t.Colors.Heading,
				Text:            t.Colors.Text,
				Background:      t.Colors.Background,
				Border:          t.Colors.Border,
				BorderFocus:     t.Colors.BorderFocus,
				Highlight:       t.Colors.Highlight,
				HighlightText:   t.Colors.HighlightText,
				Muted:           t.Colors.Muted,
				TableHeader:     t.Colors.TableHeader,
				TableHeaderLine: t.Colors.TableHeaderLine,
				StatusBarBg:     t.Colors.StatusBarBg,
				StatusBarText:   t.Colors.StatusBarText,
				Accent:          t.Colors.Accent,
				Error:           t.Colors.Error,
				Warning:         t.Colors.Warning,
			}
		}

		// Update the full config and marshal to YAML.
		s.fullConfig.UI = uiCfg

		data, err := yaml.Marshal(s.fullConfig)
		if err != nil {
			return SettingsErrMsg{err}
		}
		if err := os.WriteFile(s.configPath, data, 0o644); err != nil {
			return SettingsErrMsg{err}
		}

		// Apply the chosen theme immediately.
		SetActiveTheme(s.themeIdx)
		return SettingsSavedMsg{Theme: Themes[s.themeIdx].Name}
	}
}

// View renders the settings overlay.
func (s SettingsModel) View() string {
	panelW := s.width - 4
	if panelW < 40 {
		panelW = 40
	}
	panelH := s.height - 4
	if panelH < 20 {
		panelH = 20
	}

	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	selected := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorHighlightText())).
		Background(lipgloss.Color(ColorHighlight()))

	// ── Theme list (top section) ──────────────────────────────────────────────
	themeListStr := wrapThemeList(Themes, s.themeIdx, panelW-4, selected, muted)

	// ── Color roles (bottom section) ──────────────────────────────────────────
	var colorRows strings.Builder
	for i := 0; i < int(sfFieldCount); i++ {
		f := settingsField(i)
		name := settingsFieldNames[f]
		val := s.colorForField(s.themeIdx, f)

		var valueStr string
		if s.editMode && s.fieldIdx == i {
			valueStr = s.editBuf + "█"
		} else {
			swatch := renderSwatch(val)
			valueStr = val + " " + swatch
		}

		var row string
		label := fmt.Sprintf("%-14s", name)
		if s.fieldIdx == i && !s.editMode {
			row = selected.Render(fmt.Sprintf("  %s  %s", label, valueStr))
		} else if s.editMode && s.fieldIdx == i {
			row = heading.Render(fmt.Sprintf("  %s  %s", label, valueStr))
		} else {
			row = fmt.Sprintf("  %s  %s", muted.Render(label), valueStr)
		}
		colorRows.WriteString(row + "\n")
	}

	// ── Hint bar ──────────────────────────────────────────────────────────────
	var hints string
	if s.editMode {
		hints = "Enter:confirm  Esc:cancel"
	} else {
		hints = "←→:theme  ↑↓:role  Enter/e:edit  Ctrl+S / w:save  Esc:close"
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		heading.Render("SETTINGS — Theme & Colors"),
		"",
		heading.Render("Active theme  (←/→ to change)"),
		themeListStr,
		"",
		heading.Render("Color Roles  (↑/↓ to select, Enter/e to edit)"),
		colorRows.String(),
	)

	panel := ModalStyle(panelW, panelH).Render(body)
	hintBar := StatusBarStyle(s.width - 4).Render(hints)
	return lipgloss.JoinVertical(lipgloss.Left, panel, hintBar)
}

// wrapThemeList renders the theme names in rows that fit within maxW,
// highlighting the selected index.
func wrapThemeList(themes []Theme, activeIdx, maxW int, sel, muted lipgloss.Style) string {
	var lines []string
	var cur strings.Builder
	curW := 0
	for i, t := range themes {
		label := " " + t.Name + " "
		w := len(label) + 1
		if curW+w > maxW && curW > 0 {
			lines = append(lines, cur.String())
			cur.Reset()
			curW = 0
		}
		if i == activeIdx {
			cur.WriteString(sel.Render(label))
		} else {
			cur.WriteString(muted.Render(label))
		}
		cur.WriteString(" ")
		curW += w
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

// renderSwatch returns a colored block representing the hex color, or a
// placeholder if the color is empty.
func renderSwatch(hex string) string {
	if hex == "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("(default)")
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(hex)).
		Foreground(lipgloss.Color(hex)).
		Render("  ")
}
