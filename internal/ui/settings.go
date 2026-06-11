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

// The settings overlay is styled as a sci-fi mission console: a title bar,
// a theme selector rendered as a dropdown, segmented subsystem tabs, one
// slider row per color role (the knob position is the color's hue), an
// interactive HUE/SAT/LUM tuner with live readouts, a "signal monitor"
// strip previewing the edited theme, and a row of console action buttons.

// SettingsSavedMsg is sent after a successful config save.
type SettingsSavedMsg struct{ Theme string }

// SettingsErrMsg carries a save error back to the main model.
type SettingsErrMsg struct{ Err error }

// ── Subsystem groups ─────────────────────────────────────────────────────────

// roleGroup is one segmented tab of the console: a named subset of the Roles
// registry. Membership is derived from role names, so new roles added to the
// registry land in the right tab automatically.
type roleGroup struct {
	name  string
	roles []int // indexes into Roles
}

var settingsGroups = buildRoleGroups()

func buildRoleGroups() []roleGroup {
	groups := []roleGroup{
		{name: "GENERAL"},
		{name: "TABLES"},
		{name: "STATUS BAR"},
		{name: "ALERTS"},
	}
	for i, r := range Roles {
		gi := 0
		switch {
		case strings.HasPrefix(r.Name, "table"):
			gi = 1
		case strings.HasPrefix(r.Name, "statusBar"), strings.HasPrefix(r.Name, "hint"):
			gi = 2
		case r.Name == "error" || r.Name == "warning" || r.Name == "success" || r.Name == "info":
			gi = 3
		}
		groups[gi].roles = append(groups[gi].roles, i)
	}
	return groups
}

// roleLabel renders a camelCase role name as a spaced, uppercase console
// label: "tableSelectedBg" → "TABLE SELECTED BG".
func roleLabel(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}

// ── Model ────────────────────────────────────────────────────────────────────

// Tuner channels.
const (
	chanHue = iota
	chanSat
	chanLum
	chanHex
	numChans
)

// SettingsModel drives the settings console overlay. The list of editable
// color roles comes from the Roles registry in theme.go, so adding a role
// there automatically makes it editable here.
type SettingsModel struct {
	// Width / height of the terminal.
	width, height int

	themeIdx int // index into Themes
	fieldIdx int // selected color role (index into Roles)
	groupIdx int // active subsystem tab (index into settingsGroups)

	// Tuner state ("tune mode" captures input until applied/cancelled).
	tuneMode            bool
	tuneChan            int     // chanHue / chanSat / chanLum / chanHex
	tuneH, tuneS, tuneL float64 // staged HSL channels
	tuneHex             string  // staged hex value (kept in sync with HSL)
	tuneAuto            bool    // staged "auto" — clear the role on apply

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

// EditMode reports whether the panel is currently capturing input in the
// tuner. Callers use this to decide whether Esc should close the panel.
func (s SettingsModel) EditMode() bool { return s.tuneMode }

// colorForField reads the current in-memory color for a role from Themes[].
func (s SettingsModel) colorForField(themeIdx, fieldIdx int) string {
	return *Roles[fieldIdx].Ptr(&Themes[themeIdx].Colors)
}

// setColorForField updates the in-memory Themes[] entry.
func setColorForField(themeIdx, fieldIdx int, val string) {
	*Roles[fieldIdx].Ptr(&Themes[themeIdx].Colors) = val
	invalidateRoleCache()
}

// previewColor resolves a role for the theme being edited, preferring the
// staged tuner value when that role is under the knobs right now — so the
// signal monitor tracks every slider movement live.
func (s SettingsModel) previewColor(role string) string {
	if s.tuneMode && Roles[s.fieldIdx].Name == role {
		if s.tuneAuto {
			return ResolveRoleAt(s.themeIdx, Roles[s.fieldIdx].Fallback)
		}
		return s.stagedHex()
	}
	return ResolveRoleAt(s.themeIdx, role)
}

// stagedHex is the color the tuner would commit right now: the hex buffer
// when it parses, else the color the HSL knobs produce.
func (s SettingsModel) stagedHex() string {
	if _, _, _, ok := parseHexColor(s.tuneHex); ok {
		return strings.ToLower(strings.TrimSpace(s.tuneHex))
	}
	return hslToHex(s.tuneH, s.tuneS, s.tuneL)
}

func (s SettingsModel) Update(msg tea.Msg) (SettingsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		return s, nil
	case tea.KeyMsg:
		if s.tuneMode {
			return s.updateTuneMode(msg)
		}
		return s.updateNavMode(msg)
	}
	return s, nil
}

// group returns the active subsystem tab.
func (s SettingsModel) group() roleGroup { return settingsGroups[s.groupIdx] }

// rowInGroup returns fieldIdx's position within the active group.
func (s SettingsModel) rowInGroup() int {
	for i, ri := range s.group().roles {
		if ri == s.fieldIdx {
			return i
		}
	}
	return 0
}

func (s *SettingsModel) selectGroup(gi int) {
	if gi < 0 || gi >= len(settingsGroups) || gi == s.groupIdx {
		return
	}
	s.groupIdx = gi
	s.fieldIdx = settingsGroups[gi].roles[0]
}

func (s SettingsModel) updateNavMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if row := s.rowInGroup(); row > 0 {
			s.fieldIdx = s.group().roles[row-1]
		}
	case "down", "j":
		if row := s.rowInGroup(); row < len(s.group().roles)-1 {
			s.fieldIdx = s.group().roles[row+1]
		}
	case "left", "h":
		if s.themeIdx > 0 {
			s.themeIdx--
		}
	case "right", "l":
		if s.themeIdx < len(Themes)-1 {
			s.themeIdx++
		}
	case "tab":
		s.selectGroup((s.groupIdx + 1) % len(settingsGroups))
	case "shift+tab":
		s.selectGroup((s.groupIdx + len(settingsGroups) - 1) % len(settingsGroups))
	case "1", "2", "3", "4":
		s.selectGroup(int(msg.String()[0] - '1'))
	case "enter", "e":
		s.startTune()
	case "a":
		// Quick-reset the selected role to auto (follow its fallback chain).
		setColorForField(s.themeIdx, s.fieldIdx, "")
	case "ctrl+s", "w":
		return s, s.saveCmd()
	}
	return s, nil
}

// startTune seeds the tuner from the selected role's current value — or, for
// "auto" roles, from the color the fallback chain resolves to.
func (s *SettingsModel) startTune() {
	val := s.colorForField(s.themeIdx, s.fieldIdx)
	s.tuneAuto = val == ""
	seed := val
	if seed == "" {
		seed = ResolveRoleAt(s.themeIdx, Roles[s.fieldIdx].Name)
	}
	if h, sat, lum, ok := hexToHSL(seed); ok {
		s.tuneH, s.tuneS, s.tuneL = h, sat, lum
	} else {
		// Unparseable (named color or terminal default): start the knobs amber.
		s.tuneH, s.tuneS, s.tuneL = 42, 100, 50
	}
	if val != "" {
		s.tuneHex = strings.ToLower(val)
	} else {
		s.tuneHex = hslToHex(s.tuneH, s.tuneS, s.tuneL)
	}
	s.tuneChan = chanHue
	s.tuneMode = true
}

// adjust turns the selected knob by dir steps (negative = left). Hue wraps;
// saturation and luminance clamp.
func (s *SettingsModel) adjust(dir int, coarse bool) {
	if s.tuneChan == chanHex {
		return
	}
	hueStep, pctStep := 5.0, 2.0
	if coarse {
		hueStep, pctStep = 30.0, 10.0
	}
	switch s.tuneChan {
	case chanHue:
		s.tuneH += float64(dir) * hueStep
		for s.tuneH < 0 {
			s.tuneH += 360
		}
		for s.tuneH >= 360 {
			s.tuneH -= 360
		}
	case chanSat:
		s.tuneS = clampF(s.tuneS+float64(dir)*pctStep, 0, 100)
	case chanLum:
		s.tuneL = clampF(s.tuneL+float64(dir)*pctStep, 0, 100)
	}
	s.tuneAuto = false
	s.tuneHex = hslToHex(s.tuneH, s.tuneS, s.tuneL)
}

func (s SettingsModel) updateTuneMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		val := ""
		if !s.tuneAuto {
			val = s.stagedHex()
		}
		setColorForField(s.themeIdx, s.fieldIdx, val)
		s.tuneMode = false
	case "esc":
		s.tuneMode = false
	case "up", "k":
		s.tuneChan = (s.tuneChan + numChans - 1) % numChans
	case "down", "j":
		s.tuneChan = (s.tuneChan + 1) % numChans
	case "left", "h":
		s.adjust(-1, false)
	case "right", "l":
		s.adjust(1, false)
	case "shift+left", "H":
		s.adjust(-1, true)
	case "shift+right", "L":
		s.adjust(1, true)
	case "backspace", "ctrl+h":
		if s.tuneChan == chanHex && len(s.tuneHex) > 0 {
			s.tuneHex = s.tuneHex[:len(s.tuneHex)-1]
			s.syncFromHex()
		}
	case "a":
		// In the hex field "a" is a digit; elsewhere it toggles auto.
		if s.tuneChan == chanHex {
			s.typeHex('a')
		} else {
			s.tuneAuto = !s.tuneAuto
		}
	default:
		if s.tuneChan == chanHex {
			for _, r := range msg.Runes {
				s.typeHex(r)
			}
		}
	}
	return s, nil
}

func (s *SettingsModel) typeHex(r rune) {
	isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	if r == '#' && len(s.tuneHex) == 0 {
		s.tuneHex = "#"
		return
	}
	if !isHex || len(s.tuneHex) >= 7 {
		return
	}
	if len(s.tuneHex) == 0 {
		s.tuneHex = "#"
	}
	s.tuneHex += strings.ToLower(string(r))
	s.syncFromHex()
}

// syncFromHex re-seats the HSL knobs whenever the hex buffer becomes a
// complete, valid color.
func (s *SettingsModel) syncFromHex() {
	if h, sat, lum, ok := hexToHSL(s.tuneHex); ok {
		s.tuneH, s.tuneS, s.tuneL = h, sat, lum
		s.tuneAuto = false
	}
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
	if s.tuneMode {
		return []KeyHint{
			H("↑/↓", "knob"),
			H("←/→", "turn"),
			H("Shift+←/→", "coarse"),
			H("a", "auto"),
			H("Enter", "apply"),
			H("Esc", "cancel"),
		}
	}
	return []KeyHint{
		H("↑/↓", "role"),
		H("Tab/1-4", "subsystem"),
		H("←/→", "theme"),
		H("Enter", "tune"),
		H("a", "auto"),
		H("Ctrl+S", "save"),
		H("Esc", "close"),
	}
}

// ── Console rendering helpers ────────────────────────────────────────────────

// renderSlider draws a horizontal control: a filled run, a knob, and the
// remaining track, like a console fader. frac is the knob position in [0,1];
// fill is the color of the filled run and knob.
func renderSlider(width int, frac float64, fill string) string {
	if width < 3 {
		width = 3
	}
	pos := int(clampF(frac, 0, 1)*float64(width-1) + 0.5)
	fillSt := lipgloss.NewStyle().Foreground(lipgloss.Color(fill))
	trackSt := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))

	var b strings.Builder
	if pos > 0 {
		b.WriteString(fillSt.Render(strings.Repeat("━", pos)))
	}
	b.WriteString(fillSt.Bold(true).Render("●"))
	if rest := width - 1 - pos; rest > 0 {
		b.WriteString(trackSt.Render(strings.Repeat("─", rest)))
	}
	return b.String()
}

// renderIdleTrack draws a dimmed, dashed track for roles set to "auto" — no
// signal on this channel, the fallback drives it.
func renderIdleTrack(width int) string {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorBorder())).
		Render(strings.Repeat("┄", width))
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

// consoleRule draws a section rule with an embedded uppercase label:
// "── SIGNAL MONITOR ─────────────".
func consoleRule(width int, label string) string {
	line := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	if label == "" {
		return line.Render(strings.Repeat("─", max(width, 1)))
	}
	head := "── "
	tail := width - lipgloss.Width(head) - lipgloss.Width(label) - 1
	if tail < 1 {
		tail = 1
	}
	return line.Render(head) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())).Bold(true).Render(label) +
		" " + line.Render(strings.Repeat("─", tail))
}

// consoleButton renders one bottom-row action button: ⟨ LABEL KEY ⟩.
func consoleButton(label, key string) string {
	br := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	return br.Render("⟨ ") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText())).Bold(true).Render(label) +
		" " + lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true).Render(key) +
		br.Render(" ⟩")
}

// ── View ─────────────────────────────────────────────────────────────────────

// View renders the settings console overlay.
func (s SettingsModel) View() string {
	panelW := s.width - 6
	if panelW < 78 {
		panelW = 78
	}
	if panelW > 100 {
		panelW = 100
	}
	iw := panelW - 2 // inside the panel's Padding(0,1)

	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)

	var lines []string
	add := func(parts ...string) { lines = append(lines, strings.Join(parts, "")) }

	// ── Title bar ──
	add(heading.Render("THEME CONSOLE — DISPLAY CALIBRATION "), muted.Render("(live preview)"))
	add(consoleRule(iw, ""))

	// ── Theme selector (dropdown look) ──
	nameW := 0
	for _, t := range Themes {
		if len(t.Name) > nameW {
			nameW = len(t.Name)
		}
	}
	themeName := strings.ToUpper(Themes[s.themeIdx].Name)
	box := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	add(
		muted.Render(fmt.Sprintf("%-14s", "ACTIVE THEME")),
		accent.Render("◄ "),
		box.Render("[ "),
		text.Bold(true).Render(fmt.Sprintf("%-*s", nameW, themeName)),
		box.Render(" ▼ ]"),
		accent.Render(" ►"),
		muted.Render(fmt.Sprintf("   %02d/%02d", s.themeIdx+1, len(Themes))),
	)

	// ── Subsystem tabs (segmented buttons) ──
	var tabs []string
	for gi, g := range settingsGroups {
		label := fmt.Sprintf(" %s ", g.name)
		if gi == s.groupIdx {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorHighlightText())).
				Background(lipgloss.Color(ColorHighlight())).
				Bold(true).Render(label))
		} else {
			tabs = append(tabs, box.Render("[")+muted.Render(label)+box.Render("]"))
		}
	}
	add(muted.Render(fmt.Sprintf("%-14s", "SUBSYSTEM")), strings.Join(tabs, " "))
	add("")

	// ── Role sliders ──
	labelW := 0
	for _, r := range Roles {
		if l := len(roleLabel(r.Name)); l > labelW {
			labelW = l
		}
	}
	// marker(2) + label + gap(1) + slider + gap(2) + readout(7) + gap(1) + swatch(2)
	sliderW := iw - 2 - labelW - 1 - 2 - 7 - 1 - 2
	if sliderW < 10 {
		sliderW = 10
	}

	for _, ri := range s.group().roles {
		role := Roles[ri]
		val := s.colorForField(s.themeIdx, ri)
		selected := ri == s.fieldIdx
		// The row under the knobs tracks the tuner live, before apply.
		if s.tuneMode && selected {
			if s.tuneAuto {
				val = ""
			} else {
				val = s.stagedHex()
			}
		}

		marker := "  "
		labelSt := muted
		if selected {
			marker = accent.Render("▶ ")
			labelSt = heading
		}

		var track, readout string
		swatchColor := val
		if val == "" {
			track = renderIdleTrack(sliderW)
			readout = muted.Render(fmt.Sprintf("%-7s", "auto"))
			swatchColor = ResolveRoleAt(s.themeIdx, role.Name)
		} else if h, _, _, ok := hexToHSL(val); ok {
			track = renderSlider(sliderW, h/360, val)
			readout = text.Render(fmt.Sprintf("%-7s", strings.ToLower(val)))
		} else {
			track = renderIdleTrack(sliderW)
			readout = text.Render(fmt.Sprintf("%-7.7s", val))
		}

		add(marker, labelSt.Render(fmt.Sprintf("%-*s", labelW, roleLabel(role.Name))),
			" ", track, "  ", readout, " ", renderSwatch(swatchColor))
	}
	add("")

	// ── Tuner / role info ──
	if s.tuneMode {
		lines = append(lines, s.renderTuner(iw, labelW)...)
	} else {
		r := Roles[s.fieldIdx]
		note := r.Desc
		if r.Fallback != "" {
			note += "  ·  auto = follows " + r.Fallback
		}
		add(muted.Render("  " + note))
	}
	add("")

	// ── Signal monitor (live preview of the edited theme) ──
	lines = append(lines, s.renderMonitor(iw)...)
	add("")

	// ── Action buttons ──
	if s.tuneMode {
		add("  ",
			consoleButton("APPLY", "⏎"), "  ",
			consoleButton("AUTO", "A"), "  ",
			consoleButton("CANCEL", "ESC"))
	} else {
		add("  ",
			consoleButton("TUNE", "⏎"), "  ",
			consoleButton("AUTO", "A"), "  ",
			consoleButton("SAVE & APPLY", "^S"), "  ",
			consoleButton("CLOSE", "ESC"))
	}

	body := strings.Join(lines, "\n")
	panel := lipgloss.NewStyle().
		Width(panelW).
		MaxWidth(panelW+2).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(0, 1).
		Render(body)
	hintBar := StatusBar(panelW+2, "", s.settingsHints())
	return lipgloss.JoinVertical(lipgloss.Left, panel, hintBar)
}

// renderTuner draws the HUE/SAT/LUM/HEX knobs for the selected role.
func (s SettingsModel) renderTuner(iw, labelW int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning())).Bold(true)

	staged := s.stagedHex()
	role := Roles[s.fieldIdx]
	var out []string
	out = append(out, consoleRule(iw, "TUNE · "+roleLabel(role.Name)))

	// Channel rows: label, fader, readout. Each fader is tinted with the color
	// it would produce, so the knobs feel physical.
	chanW := iw - 2 - 4 - 1 - 2 - 5 - 4
	if chanW < 10 {
		chanW = 10
	}
	row := func(ch int, name string, frac float64, fill, readout string) string {
		marker := "  "
		nameSt := muted
		if s.tuneChan == ch {
			marker = accent.Render("▶ ")
			nameSt = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHeading())).Bold(true)
		}
		track := renderSlider(chanW, frac, fill)
		if s.tuneAuto {
			track = renderIdleTrack(chanW)
		}
		return marker + nameSt.Render(fmt.Sprintf("%-4s", name)) + " " + track + "  " +
			text.Render(fmt.Sprintf("%5s", readout))
	}
	out = append(out,
		row(chanHue, "HUE", s.tuneH/360, hslToHex(s.tuneH, 100, 50), fmt.Sprintf("%03d°", int(s.tuneH))),
		row(chanSat, "SAT", s.tuneS/100, hslToHex(s.tuneH, s.tuneS, 50), fmt.Sprintf("%3d%%", int(s.tuneS))),
		row(chanLum, "LUM", s.tuneL/100, hslToHex(0, 0, s.tuneL), fmt.Sprintf("%3d%%", int(s.tuneL))),
	)

	// Hex row + staged output preview.
	hexMarker := "  "
	hexSt := muted
	cursor := ""
	if s.tuneChan == chanHex {
		hexMarker = accent.Render("▶ ")
		hexSt = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHeading())).Bold(true)
		cursor = accent.Render("█")
	}
	stagedSt := lipgloss.NewStyle().Foreground(lipgloss.Color(staged))
	out = append(out, hexMarker+hexSt.Render(fmt.Sprintf("%-4s", "HEX"))+" "+
		text.Render(s.tuneHex)+cursor+
		strings.Repeat(" ", max(2, 10-len(s.tuneHex)))+
		stagedSt.Render("▉▉▉▉▉ sample"))

	if s.tuneAuto {
		note := "AUTO — value cleared on apply"
		if role.Fallback != "" {
			note += "; follows " + role.Fallback
		}
		out = append(out, "  "+warn.Render(note))
	}
	return out
}

// renderMonitor draws the live preview strip: a miniature header, table and
// status bar rendered with the edited theme's colors, so every adjustment is
// visible before saving.
func (s SettingsModel) renderMonitor(iw int) []string {
	pc := s.previewColor
	fg := func(role string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(pc(role)))
	}

	out := []string{consoleRule(iw, "SIGNAL MONITOR")}

	// Header sample: heading, body, muted and accent text side by side.
	out = append(out, "  "+
		fg("heading").Bold(true).Render("◈ AWS EXPLORER")+"  "+
		fg("text").Render("body text")+"  "+
		fg("muted").Render("muted text")+"  "+
		fg("accent").Render("━ accent ━"))

	// Table sample: header row, a plain cell and the selected row.
	hdr := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("tableHeader"))).
		Background(lipgloss.Color(pc("tableHeaderBg"))).
		Bold(true)
	sel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("tableSelectedText"))).
		Background(lipgloss.Color(pc("tableSelectedBg")))
	out = append(out, "  "+
		hdr.Render(" SERVICE  REGION     STATE   ")+"  "+
		fg("tableText").Render("ec2  us-east-1  running")+"  "+
		sel.Render(" ▶ s3  us-east-1  active "))

	// Status bar + alert samples.
	bar := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("statusBarText"))).
		Background(lipgloss.Color(pc("statusBarBg")))
	key := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("hintKey"))).
		Background(lipgloss.Color(pc("statusBarBg"))).
		Bold(true)
	hint := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("hintText"))).
		Background(lipgloss.Color(pc("statusBarBg")))
	out = append(out, "  "+
		bar.Render(" status ")+key.Render("Enter")+hint.Render(" open ")+
		bar.Render(" ")+"  "+
		fg("success").Render("✓ ok")+" "+
		fg("warning").Render("⚠ warn")+" "+
		fg("error").Render("✗ fail")+" "+
		fg("info").Render("ℹ info"))

	return out
}
