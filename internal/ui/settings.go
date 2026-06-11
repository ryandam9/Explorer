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

// SettingsModel drives the settings overlay panel. The list of editable color
// roles comes from the Roles registry in theme.go, so adding a role there
// automatically makes it editable here.
type SettingsModel struct {
	// Width / height of the terminal.
	width, height int

	// Theme list navigation.
	themeIdx int // index into Themes
	fieldIdx int // which color role is being edited (index into Roles)
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
func (s SettingsModel) colorForField(themeIdx, fieldIdx int) string {
	return *Roles[fieldIdx].Ptr(&Themes[themeIdx].Colors)
}

// setColorForField updates the in-memory Themes[] entry.
func setColorForField(themeIdx, fieldIdx int, val string) {
	*Roles[fieldIdx].Ptr(&Themes[themeIdx].Colors) = val
	invalidateRoleCache()
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
		if s.fieldIdx < len(Roles)-1 {
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
		s.editBuf = s.colorForField(s.themeIdx, s.fieldIdx)
	case "ctrl+s", "w":
		return s, s.saveCmd()
	}
	return s, nil
}

func (s SettingsModel) updateEditMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		setColorForField(s.themeIdx, s.fieldIdx, strings.TrimSpace(s.editBuf))
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

		// Build the updated UI config from the in-memory Themes slice. Only
		// non-empty roles are written, so roles left on "auto" keep following
		// their fallback chain.
		uiCfg := config.UIConfig{
			Theme:  Themes[s.themeIdx].Name,
			Themes: make(map[string]map[string]string, len(Themes)),
		}
		for _, t := range Themes {
			colors := make(map[string]string, len(Roles))
			for _, r := range Roles {
				if v := *r.Ptr(&t.Colors); v != "" {
					colors[r.Name] = v
				}
			}
			uiCfg.Themes[t.Name] = colors
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

// settingsHints returns the context-aware shortcuts for the panel's current
// input mode.
func (s SettingsModel) settingsHints() []KeyHint {
	if s.editMode {
		return []KeyHint{
			H("Enter", "confirm"),
			H("Esc", "cancel"),
		}
	}
	return []KeyHint{
		H("←/→", "theme"),
		H("↑/↓", "role"),
		H("Enter", "edit"),
		H("Ctrl+S", "save"),
		H("Esc", "close"),
	}
}

// renderRoleRow renders one role line: name, color swatch and value (or the
// in-progress edit buffer).
func (s SettingsModel) renderRoleRow(i, nameW int, heading, muted, selected lipgloss.Style) string {
	r := Roles[i]
	val := s.colorForField(s.themeIdx, i)

	// "auto" rows preview the color the fallback chain resolves to.
	text := val
	swatchColor := val
	if val == "" {
		text = "auto"
		swatchColor = ResolveRoleAt(s.themeIdx, r.Name)
	}

	label := fmt.Sprintf("%-*s", nameW, r.Name)
	// The swatch carries its own background color, so it is appended outside
	// the row style to keep the selection highlight intact.
	switch {
	case s.editMode && s.fieldIdx == i:
		return heading.Render(fmt.Sprintf(" %s %s█", label, s.editBuf))
	case s.fieldIdx == i:
		return selected.Render(fmt.Sprintf(" %s %-8s", label, text)) + " " + renderSwatch(swatchColor)
	default:
		return " " + muted.Render(label) + " " + fmt.Sprintf("%-8s", text) + " " + renderSwatch(swatchColor)
	}
}

// View renders the settings overlay.
func (s SettingsModel) View() string {
	panelW := s.width - 4
	if panelW < 72 {
		panelW = 72
	}

	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	selected := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(ColorHighlightText())).
		Background(lipgloss.Color(ColorHighlight()))

	// ── Theme list (top section) ──────────────────────────────────────────────
	themeListStr := wrapThemeList(Themes, s.themeIdx, panelW-4, selected, muted)

	// ── Color roles (bottom section, two columns) ─────────────────────────────
	nameW := 0
	for _, r := range Roles {
		if len(r.Name) > nameW {
			nameW = len(r.Name)
		}
	}
	half := (len(Roles) + 1) / 2
	var leftRows, rightRows []string
	for i := 0; i < half; i++ {
		leftRows = append(leftRows, s.renderRoleRow(i, nameW, heading, muted, selected))
	}
	for i := half; i < len(Roles); i++ {
		rightRows = append(rightRows, s.renderRoleRow(i, nameW, heading, muted, selected))
	}
	colW := (panelW - 6) / 2
	leftCol := lipgloss.NewStyle().Width(colW).Render(strings.Join(leftRows, "\n"))
	rightCol := lipgloss.NewStyle().Width(colW).Render(strings.Join(rightRows, "\n"))
	colorRows := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "  ", rightCol)

	fallbackNote := ""
	if !s.editMode {
		r := Roles[s.fieldIdx]
		fallbackNote = r.Desc
		if r.Fallback != "" {
			fallbackNote += "  ·  auto = follows " + r.Fallback
		}
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		heading.Render("SETTINGS — Theme & Colors"),
		"",
		heading.Render("Active theme  (←/→ to change)"),
		themeListStr,
		"",
		heading.Render("Color Roles  (↑/↓ to select, Enter/e to edit)"),
		colorRows,
		"",
		muted.Render(fallbackNote),
	)

	panel := lipgloss.NewStyle().
		Width(panelW).
		MaxWidth(panelW+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(1, 2).
		Render(body)
	hintBar := StatusBar(panelW+2, "", s.settingsHints())
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
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())).Render("··")
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color(hex)).
		Foreground(lipgloss.Color(hex)).
		Render("  ")
}
