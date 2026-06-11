package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v3"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// The settings overlay is an "Appearance" panel floated over the live app,
// laid out like a design-tool settings dialog: a theme selector and section
// tabs up top, then two columns — the section's color roles on the left
// (name, dot leaders, value, swatch) and a bordered live-preview card on the
// right showing a miniature header, table and status bar in the edited theme.
// Below them sits a context strip: a quick swatch palette in nav mode, the
// theme bank on the theme row, and gradient Hue/Sat/Lum faders while tuning.
// Every row is a control: ↑/↓ selects, ←/→ changes its value — instantly.
//
// The panel never resizes: its width and height are fixed regardless of
// terminal size, active tab or input mode.

// SettingsSavedMsg is sent after a successful config save.
type SettingsSavedMsg struct{ Theme string }

// SettingsErrMsg carries a save error back to the main model.
type SettingsErrMsg struct{ Err error }

// ── Fixed panel geometry ─────────────────────────────────────────────────────

const (
	consoleWidth = 86           // outer panel width (excluding border)
	consoleInner = consoleWidth // content width inside Padding(0,1)
	ctrlZoneRows = 6            // tuner / palette zone height
	leftColWidth = 42           // role-list column width in the two-column body
)

// ── Section groups ───────────────────────────────────────────────────────────

// roleGroup is one section tab of the panel: a named subset of the Roles
// registry. Membership is derived from role names, so new roles added to the
// registry land in the right tab automatically.
type roleGroup struct {
	name  string
	roles []int // indexes into Roles
}

var settingsGroups = buildRoleGroups()

func buildRoleGroups() []roleGroup {
	groups := []roleGroup{
		{name: "General"},
		{name: "Tables"},
		{name: "Status bar"},
		{name: "Alerts"},
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

// maxGroupRows is the row count of the largest section tab; smaller tabs are
// padded to it so the panel height never changes when switching tabs.
var maxGroupRows = func() int {
	m := 0
	for _, g := range settingsGroups {
		if len(g.roles) > m {
			m = len(g.roles)
		}
	}
	return m
}()

// roleLabel renders a camelCase role name as a spaced, sentence-case label
// with abbreviations expanded: "tableSelectedBg" → "Table selected background".
func roleLabel(name string) string {
	var words []string
	start := 0
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			words = append(words, strings.ToLower(name[start:i]))
			start = i
		}
	}
	words = append(words, strings.ToLower(name[start:]))
	for i, w := range words {
		if w == "bg" {
			words[i] = "background"
		}
	}
	out := strings.Join(words, " ")
	return strings.ToUpper(out[:1]) + out[1:]
}

// ── Quick palette ────────────────────────────────────────────────────────────

// quickPalette is the swatch ring ←/→ cycles through on a role row: the
// edited theme's own colors first (the hues the palette was designed around),
// then a 12-step hue wheel and a gray ramp for everything else.
func quickPalette(themeIdx int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.ToLower(strings.TrimSpace(c))
		if _, _, _, ok := parseHexColor(c); !ok {
			return
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, r := range Roles {
		add(*r.Ptr(&Themes[themeIdx].Colors))
	}
	for h := 0; h < 360; h += 30 {
		add(hslToHex(float64(h), 85, 55))
	}
	for _, g := range []string{"#000000", "#3a3a3a", "#6e6e6e", "#a0a0a0", "#d0d0d0", "#ffffff"} {
		add(g)
	}
	return out
}

// nearestSwatch returns the palette index closest to hex by RGB distance.
func nearestSwatch(palette []string, hex string) int {
	r0, g0, b0, ok := parseHexColor(hex)
	if !ok {
		return 0
	}
	best, bestD := 0, 1<<30
	for i, p := range palette {
		r, g, b, ok := parseHexColor(p)
		if !ok {
			continue
		}
		d := (r-r0)*(r-r0) + (g-g0)*(g-g0) + (b-b0)*(b-b0)
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
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
	// Terminal size (kept for callers; the console itself is fixed-size).
	width, height int

	themeIdx int  // index into Themes
	fieldIdx int  // selected color role (index into Roles)
	groupIdx int  // active subsystem tab (index into settingsGroups)
	onTheme  bool // cursor is on the ACTIVE THEME row, above the roles

	// palette is the quick-swatch ring, snapshotted when the console opens or
	// the theme changes. A snapshot keeps the ring stable while cycling: the
	// ring is built from the theme's colors, so deriving it live would reshape
	// it under the user with every change.
	palette []string

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
		palette:    quickPalette(getActiveTheme()),
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

// selectTheme switches the edited theme and applies it live, so the entire
// app — visible around the floating console — restyles instantly. Ctrl+S
// persists it to config.yaml.
func (s *SettingsModel) selectTheme(idx int) {
	if idx < 0 || idx >= len(Themes) || idx == s.themeIdx {
		return
	}
	s.themeIdx = idx
	s.palette = quickPalette(idx)
	SetActiveTheme(idx)
}

// cycleSwatch is the one-keystroke color change: step the selected role
// through the quick palette and apply immediately. A color that is not in
// the palette snaps to its nearest swatch first.
func (s *SettingsModel) cycleSwatch(dir int) {
	pal := s.palette
	if len(pal) == 0 {
		return
	}
	cur := strings.ToLower(s.colorForField(s.themeIdx, s.fieldIdx))
	if cur == "" {
		cur = strings.ToLower(ResolveRoleAt(s.themeIdx, Roles[s.fieldIdx].Name))
	}
	idx := -1
	for i, p := range pal {
		if p == cur {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = nearestSwatch(pal, cur)
	} else {
		idx = (idx + dir + len(pal)) % len(pal)
	}
	setColorForField(s.themeIdx, s.fieldIdx, pal[idx])
}

func (s SettingsModel) updateNavMode(msg tea.KeyMsg) (SettingsModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if s.onTheme {
			break
		}
		if row := s.rowInGroup(); row > 0 {
			s.fieldIdx = s.group().roles[row-1]
		} else {
			s.onTheme = true
		}
	case "down", "j":
		if s.onTheme {
			s.onTheme = false
			s.fieldIdx = s.group().roles[0]
		} else if row := s.rowInGroup(); row < len(s.group().roles)-1 {
			s.fieldIdx = s.group().roles[row+1]
		}
	case "left", "h":
		if s.onTheme {
			s.selectTheme(s.themeIdx - 1)
		} else {
			s.cycleSwatch(-1)
		}
	case "right", "l":
		if s.onTheme {
			s.selectTheme(s.themeIdx + 1)
		} else {
			s.cycleSwatch(1)
		}
	case "tab":
		s.selectGroup((s.groupIdx + 1) % len(settingsGroups))
	case "shift+tab":
		s.selectGroup((s.groupIdx + len(settingsGroups) - 1) % len(settingsGroups))
	case "1", "2", "3", "4":
		s.selectGroup(int(msg.String()[0] - '1'))
	case "enter", "e":
		if s.onTheme {
			s.onTheme = false
			s.fieldIdx = s.group().roles[0]
		} else {
			s.startTune()
		}
	case "a":
		// Quick-reset the selected role to auto (follow its fallback chain).
		if !s.onTheme {
			setColorForField(s.themeIdx, s.fieldIdx, "")
		}
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
		// The app runs without a config file (built-in defaults); the first
		// save may have to create the user config directory.
		if dir := filepath.Dir(s.configPath); dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return SettingsErrMsg{err}
			}
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
			H("↑/↓", "channel"),
			H("←/→", "adjust"),
			H("Shift+←/→", "coarse"),
			H("a", "auto"),
			H("Enter", "apply"),
			H("Esc", "cancel"),
		}
	}
	return []KeyHint{
		H("↑/↓", "row"),
		H("←/→", "change"),
		H("Tab/1-4", "section"),
		H("Enter", "tune"),
		H("a", "auto"),
		H("Ctrl+S", "save"),
		H("Esc", "close"),
	}
}

// ── Panel rendering helpers ──────────────────────────────────────────────────

// knobFg picks a knob color that stays visible on top of the given cell
// color: black on light cells, white on dark ones.
func knobFg(hex string) string {
	if _, _, l, ok := hexToHSL(hex); ok && l > 60 {
		return "#000000"
	}
	return "#ffffff"
}

// renderFader draws a gradient fader: every cell is painted with the color
// that position would produce (via colorAt, f in [0,1]), so the control shows
// the actual range being chosen rather than an abstract track. The knob sits
// at frac.
func renderFader(width int, frac float64, colorAt func(f float64) string) string {
	if width < 3 {
		width = 3
	}
	pos := int(clampF(frac, 0, 1)*float64(width-1) + 0.5)
	var b strings.Builder
	for i := 0; i < width; i++ {
		f := float64(i) / float64(width-1)
		c := colorAt(f)
		cell := lipgloss.NewStyle().Background(lipgloss.Color(c))
		if i == pos {
			b.WriteString(cell.Foreground(lipgloss.Color(knobFg(c))).Bold(true).Render("●"))
		} else {
			b.WriteString(cell.Render(" "))
		}
	}
	return b.String()
}

// padTo pads s with spaces to exactly w terminal cells (ANSI-aware), so
// multi-column layouts stay aligned regardless of styling.
func padTo(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
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

// consoleRule draws a section rule with an embedded label:
// "── Palette · Heading ─────────────".
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

// padZone pads (or truncates) a section to exactly rows lines, so the panel
// height never changes between modes and tabs.
func padZone(lines []string, rows int) []string {
	for len(lines) < rows {
		lines = append(lines, "")
	}
	return lines[:rows]
}

// ── View ─────────────────────────────────────────────────────────────────────

// View renders the Appearance panel overlay at its fixed size.
func (s SettingsModel) View() string {
	iw := consoleInner - 2 // inside the panel's Padding(0,1)

	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)

	var lines []string
	add := func(parts ...string) { lines = append(lines, strings.Join(parts, "")) }

	// ── Title row ──
	title := heading.Render("Appearance")
	note := muted.Render("changes apply live · Ctrl+S saves")
	gap := iw - lipgloss.Width(title) - lipgloss.Width(note)
	if gap < 1 {
		gap = 1
	}
	add(title, strings.Repeat(" ", gap), note)
	add(consoleRule(iw, ""))

	// ── Theme selector (a control row: ←/→ switches and applies live) ──
	nameW := 0
	for _, t := range Themes {
		if len(t.Name) > nameW {
			nameW = len(t.Name)
		}
	}
	themeMarker := "  "
	themeLabelSt := muted
	if s.onTheme && !s.tuneMode {
		themeMarker = accent.Render("❯ ")
		themeLabelSt = heading
	}
	dots := s.renderThemeDots(6)
	// Chevrons hug the name; the row is padded after them so the counter and
	// palette dots stay put while cycling through themes.
	themeName := accent.Render("‹ ") + text.Bold(true).Render(Themes[s.themeIdx].Name) + accent.Render(" ›")
	add(
		themeMarker,
		themeLabelSt.Render(fmt.Sprintf("%-10s", "Theme")),
		padTo(themeName, nameW+4),
		muted.Render(fmt.Sprintf("  %2d/%d", s.themeIdx+1, len(Themes))),
		"   ", dots,
	)

	// ── Section tabs ──
	var tabs []string
	for gi, g := range settingsGroups {
		label := fmt.Sprintf(" %s ", g.name)
		if gi == s.groupIdx {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color(ColorHighlightText())).
				Background(lipgloss.Color(ColorHighlight())).
				Bold(true).Render(label))
		} else {
			tabs = append(tabs, muted.Render(label))
		}
	}
	add("  ", muted.Render(fmt.Sprintf("%-10s", "Section")), strings.Join(tabs, " "))
	add("")

	// ── Two-column body: role list left, live preview card right ──
	leftLines := s.renderRoleList(leftColWidth)
	rightLines := s.renderPreviewCard(iw - leftColWidth - 2)
	bodyRows := max(maxGroupRows, len(rightLines))
	leftLines = padZone(leftLines, bodyRows)
	rightLines = padZone(rightLines, bodyRows)
	for i := 0; i < bodyRows; i++ {
		add(padTo(leftLines[i], leftColWidth), "  ", rightLines[i])
	}
	add("")

	// ── Context strip: tuner, quick palette, or theme bank (fixed height) ──
	switch {
	case s.tuneMode:
		lines = append(lines, padZone(s.renderTuner(iw), ctrlZoneRows)...)
	case s.onTheme:
		lines = append(lines, padZone(s.renderThemeBank(iw), ctrlZoneRows)...)
	default:
		lines = append(lines, padZone(s.renderQuickPalette(iw), ctrlZoneRows)...)
	}

	body := strings.Join(lines, "\n")
	panel := lipgloss.NewStyle().
		Width(consoleWidth).
		MaxWidth(consoleWidth+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(0, 1).
		Render(body)
	hintBar := StatusBar(consoleWidth+2, "", s.settingsHints())
	return lipgloss.JoinVertical(lipgloss.Left, panel, hintBar)
}

// renderThemeDots draws up to n distinct color dots from the edited theme —
// a one-glance fingerprint of the palette next to the theme name.
func (s SettingsModel) renderThemeDots(n int) string {
	var b strings.Builder
	seen := map[string]bool{}
	count := 0
	for _, r := range Roles {
		c := strings.ToLower(*r.Ptr(&Themes[s.themeIdx].Colors))
		if c == "" || seen[c] || count >= n {
			continue
		}
		if _, _, _, ok := parseHexColor(c); !ok {
			continue
		}
		seen[c] = true
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("●"))
		b.WriteString(" ")
		count++
	}
	return strings.TrimRight(b.String(), " ")
}

// renderRoleList draws the active section's color roles, one per row:
// marker, label, dot leaders, value, swatch.
func (s SettingsModel) renderRoleList(width int) []string {
	heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)
	leader := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))

	var rows []string
	for _, ri := range s.group().roles {
		role := Roles[ri]
		val := s.colorForField(s.themeIdx, ri)
		selected := ri == s.fieldIdx && !s.onTheme
		// The row under the tuner tracks every adjustment live, before apply.
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
			marker = accent.Render("❯ ")
			labelSt = heading
		}

		var readout string
		swatchColor := val
		if val == "" {
			readout = muted.Render(fmt.Sprintf("%-7s", "auto"))
			swatchColor = ResolveRoleAt(s.themeIdx, role.Name)
		} else {
			readout = text.Render(fmt.Sprintf("%-7.7s", strings.ToLower(val)))
		}

		label := roleLabel(role.Name)
		// marker(2) + label + sp + leaders + sp + value(7) + sp + swatch(2)
		leaders := width - 2 - len(label) - 1 - 1 - 7 - 1 - 2
		if leaders < 1 {
			leaders = 1
		}
		rows = append(rows,
			marker+labelSt.Render(label)+" "+
				leader.Render(strings.Repeat("·", leaders))+" "+
				readout+" "+renderSwatch(swatchColor))
	}
	return rows
}

// renderQuickPalette draws the swatch ring for the selected role with a caret
// under the active swatch, plus the role's description.
func (s SettingsModel) renderQuickPalette(iw int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)

	role := Roles[s.fieldIdx]
	pal := s.palette
	cur := strings.ToLower(s.colorForField(s.themeIdx, s.fieldIdx))
	if cur == "" {
		cur = strings.ToLower(ResolveRoleAt(s.themeIdx, role.Name))
	}
	curIdx := -1
	for i, p := range pal {
		if p == cur {
			curIdx = i
			break
		}
	}

	var strip, caret strings.Builder
	strip.WriteString("  ")
	caret.WriteString("  ")
	for i, p := range pal {
		if i >= iw-4 { // never wider than the console
			break
		}
		strip.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(p)).Render("█"))
		if i == curIdx {
			caret.WriteString(accent.Render("▲"))
		} else {
			caret.WriteString(" ")
		}
	}

	note := role.Desc
	if role.Fallback != "" {
		note += "  ·  auto follows " + roleLabel(role.Fallback)
	}
	return []string{
		consoleRule(iw, "Palette · "+roleLabel(role.Name)),
		strip.String(),
		caret.String(),
		muted.Render("  " + note),
		muted.Render("  ←/→ swap color instantly  ·  Enter opens the hue/sat/lum tuner"),
	}
}

// renderThemeBank draws the context strip for the theme row: the neighbouring
// theme names, the selected theme's palette and what ←/→ does there.
func (s SettingsModel) renderThemeBank(iw int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText())).Bold(true)
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)

	// Neighbour line: "prev-theme  ‹ current ›  next-theme".
	var nav strings.Builder
	nav.WriteString("  ")
	if s.themeIdx > 0 {
		nav.WriteString(muted.Render(Themes[s.themeIdx-1].Name) + "  ")
	}
	nav.WriteString(accent.Render("‹ ") + text.Render(Themes[s.themeIdx].Name) + accent.Render(" ›"))
	if s.themeIdx < len(Themes)-1 {
		nav.WriteString("  " + muted.Render(Themes[s.themeIdx+1].Name))
	}

	var strip strings.Builder
	strip.WriteString("  ")
	seen := map[string]bool{}
	n := 0
	for _, r := range Roles {
		c := strings.ToLower(*r.Ptr(&Themes[s.themeIdx].Colors))
		if c == "" || seen[c] || n >= iw-4 {
			continue
		}
		seen[c] = true
		strip.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("██"))
		strip.WriteString(" ")
		n += 3
	}

	return []string{
		consoleRule(iw, "Themes"),
		nav.String(),
		strip.String(),
		muted.Render("  " + fmt.Sprintf("%d bird palettes — colors from the feathers project", len(Themes))),
		muted.Render("  ←/→ switch theme, the whole app restyles instantly  ·  Ctrl+S saves"),
	}
}

// renderTuner draws the Hue/Sat/Lum/Hex channels for the selected role. Each
// fader is a true gradient — every cell painted with the color that position
// would produce — so the control shows the range, not an abstract track.
func (s SettingsModel) renderTuner(iw int) []string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	text := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorAccent())).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorWarning())).Bold(true)

	staged := s.stagedHex()
	role := Roles[s.fieldIdx]
	var out []string
	out = append(out, consoleRule(iw, "Tune · "+roleLabel(role.Name)))

	// Channel rows: label, gradient fader, readout.
	chanW := iw - 2 - 4 - 1 - 2 - 5 - 4
	if chanW < 10 {
		chanW = 10
	}
	row := func(ch int, name string, frac float64, colorAt func(float64) string, readout string) string {
		marker := "  "
		nameSt := muted
		if s.tuneChan == ch {
			marker = accent.Render("❯ ")
			nameSt = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHeading())).Bold(true)
		}
		track := renderFader(chanW, frac, colorAt)
		if s.tuneAuto {
			track = renderIdleTrack(chanW)
		}
		return marker + nameSt.Render(fmt.Sprintf("%-4s", name)) + " " + track + "  " +
			text.Render(fmt.Sprintf("%5s", readout))
	}
	out = append(out,
		row(chanHue, "Hue", s.tuneH/360,
			func(f float64) string { return hslToHex(f*360, 100, 50) },
			fmt.Sprintf("%03d°", int(s.tuneH))),
		row(chanSat, "Sat", s.tuneS/100,
			func(f float64) string { return hslToHex(s.tuneH, f*100, 50) },
			fmt.Sprintf("%3d%%", int(s.tuneS))),
		row(chanLum, "Lum", s.tuneL/100,
			func(f float64) string { return hslToHex(s.tuneH, s.tuneS, f*100) },
			fmt.Sprintf("%3d%%", int(s.tuneL))),
	)

	// Hex row + staged output preview.
	hexMarker := "  "
	hexSt := muted
	cursor := ""
	if s.tuneChan == chanHex {
		hexMarker = accent.Render("❯ ")
		hexSt = lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHeading())).Bold(true)
		cursor = accent.Render("█")
	}
	stagedSt := lipgloss.NewStyle().Foreground(lipgloss.Color(staged))
	out = append(out, hexMarker+hexSt.Render(fmt.Sprintf("%-4s", "Hex"))+" "+
		text.Render(s.tuneHex)+cursor+
		strings.Repeat(" ", max(2, 10-len(s.tuneHex)))+
		stagedSt.Render("▉▉▉▉▉ result"))

	if s.tuneAuto {
		note := "auto — value cleared on apply"
		if role.Fallback != "" {
			note += "; follows " + roleLabel(role.Fallback)
		}
		out = append(out, "  "+warn.Render(note))
	}
	return out
}

// renderPreviewCard draws a bordered card containing a miniature app — header,
// table, status bar and alerts — rendered with the edited theme's colors, so
// every adjustment is visible in context before saving.
func (s SettingsModel) renderPreviewCard(width int) []string {
	pc := s.previewColor
	fg := func(role string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(pc(role)))
	}
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorBorder()))
	mutedB := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())).Bold(true)
	inner := width - 4 // borders + one space of padding each side

	var content []string

	// Header sample: heading, body, muted and accent text side by side.
	content = append(content,
		fg("heading").Bold(true).Render("◈ AWS Explorer")+"  "+
			fg("text").Render("text")+" "+
			fg("muted").Render("muted")+" "+
			fg("accent").Render("accent"))

	// Table sample: header row, a plain row and the selected row.
	hdr := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("tableHeader"))).
		Background(lipgloss.Color(pc("tableHeaderBg"))).
		Bold(true)
	sel := lipgloss.NewStyle().
		Foreground(lipgloss.Color(pc("tableSelectedText"))).
		Background(lipgloss.Color(pc("tableSelectedBg")))
	content = append(content,
		hdr.Render(padTo(" SERVICE    REGION       STATE", inner)),
		fg("tableText").Render(" ec2        us-east-1    running"),
		sel.Render(padTo(" ❯ s3       us-east-1    active", inner)),
		"")

	// Status bar sample, padded to a full-width solid bar.
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
	barLine := bar.Render(" ") + key.Render("Enter") + hint.Render(" open") +
		bar.Render(" · ") + key.Render("q") + hint.Render(" quit")
	if rest := inner - lipgloss.Width(barLine); rest > 0 {
		barLine += bar.Render(strings.Repeat(" ", rest))
	}
	content = append(content, barLine)

	// Alert samples.
	content = append(content,
		fg("success").Render("✓ ok")+"  "+
			fg("warning").Render("⚠ warn")+"  "+
			fg("error").Render("✗ fail")+"  "+
			fg("info").Render("ℹ info"))

	// Frame the card with an embedded "Preview" title.
	titleStr := "Preview"
	fill := width - 3 - len(titleStr) - 1 - 1
	if fill < 1 {
		fill = 1
	}
	out := []string{
		border.Render("╭─ ") + mutedB.Render(titleStr) + border.Render(" "+strings.Repeat("─", fill)+"╮"),
	}
	for _, c := range content {
		out = append(out, border.Render("│")+" "+padTo(c, inner)+" "+border.Render("│"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return out
}
