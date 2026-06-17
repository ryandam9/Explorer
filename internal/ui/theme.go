package ui

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v3"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// ThemeColors holds the full color palette for a theme.
//
// Colors are taken from the "feathers" palettes (color schemes inspired by the
// plumage of Australian birds — https://github.com/shandiya/feathers, the same
// data rendered at https://ryandam.net/demos/feathers_palettes/). Each role is
// granular so editing one UI area never bleeds into another. Roles that are
// empty fall back to a related role at render time (see Roles), so themes and
// user configs only need to set the knobs they care about.
type ThemeColors struct {
	Heading       string // titles, section headers
	Text          string // body text / foreground
	Background    string // panel background (empty = terminal default)
	Border        string // borders of unfocused panels
	BorderFocus   string // border of the focused panel
	Highlight     string // selected item background (lists, menus)
	HighlightText string // text on the selected item
	Muted         string // de-emphasised / secondary text
	Accent        string // decorative rails, input prompts and cursors

	// Table roles. These theme every table in the application identically.
	TableHeader       string // table column header text
	TableHeaderBg     string // table column header background
	TableHeaderLine   string // rule drawn under table headers
	TableText         string // table cell text
	TableBorder       string // border drawn around table panels
	TableSelectedBg   string // selected table row background
	TableSelectedText string // selected table row text
	TableRowAltBg     string // zebra background for alternate (odd) data rows

	// Status bar roles, including the context-shortcut hints rendered in it.
	StatusBarBg   string // status bar background
	StatusBarText string // status bar text
	HintKey       string // shortcut key (e.g. "Enter") in the status bar hints
	HintText      string // shortcut description (e.g. "open") in the hints

	// Alert roles.
	Error   string // error messages and indicators
	Warning string // warning messages and indicators
	Success string // success / confirmation messages and indicators
	Info    string // informational messages and indicators
}

// Theme holds a named theme with its full color palette.
type Theme struct {
	Name   string
	Colors ThemeColors
}

// RoleSpec describes one themable color role: its canonical config key, a
// short human description (shown in the settings panel), an accessor for the
// backing ThemeColors field, and the role its value falls back to when unset.
type RoleSpec struct {
	// Name is the canonical key used in themes.yaml and config.yaml
	// (ui.themes.<theme>.<name>). Matching is case-insensitive.
	Name string
	// Desc is a short description shown in the settings panel.
	Desc string
	// Ptr returns the address of the backing field inside a ThemeColors.
	Ptr func(*ThemeColors) *string
	// Fallback is the Name of the role consulted when this one is empty.
	// Empty means "no fallback" (the terminal default is used).
	Fallback string
}

// Roles is the registry of every themable color role. It is the single place
// new roles are declared: the settings panel, config overrides and themes.yaml
// parsing all iterate this list.
var Roles = []RoleSpec{
	{"heading", "titles & section headers", func(c *ThemeColors) *string { return &c.Heading }, ""},
	{"text", "body text", func(c *ThemeColors) *string { return &c.Text }, ""},
	{"background", "panel background", func(c *ThemeColors) *string { return &c.Background }, ""},
	{"muted", "secondary text", func(c *ThemeColors) *string { return &c.Muted }, ""},
	{"accent", "rails, prompts, cursors", func(c *ThemeColors) *string { return &c.Accent }, "heading"},
	{"border", "unfocused panel border", func(c *ThemeColors) *string { return &c.Border }, ""},
	{"borderFocus", "focused panel border", func(c *ThemeColors) *string { return &c.BorderFocus }, "heading"},
	{"highlight", "selected item background", func(c *ThemeColors) *string { return &c.Highlight }, ""},
	{"highlightText", "selected item text", func(c *ThemeColors) *string { return &c.HighlightText }, ""},
	{"tableHeader", "table header text", func(c *ThemeColors) *string { return &c.TableHeader }, "muted"},
	{"tableHeaderBg", "table header background", func(c *ThemeColors) *string { return &c.TableHeaderBg }, "background"},
	{"tableHeaderLine", "rule under table header", func(c *ThemeColors) *string { return &c.TableHeaderLine }, "border"},
	{"tableText", "table cell text", func(c *ThemeColors) *string { return &c.TableText }, "text"},
	{"tableBorder", "table panel border", func(c *ThemeColors) *string { return &c.TableBorder }, "border"},
	{"tableSelectedBg", "selected row background", func(c *ThemeColors) *string { return &c.TableSelectedBg }, "highlight"},
	{"tableSelectedText", "selected row text", func(c *ThemeColors) *string { return &c.TableSelectedText }, "highlightText"},
	{"tableRowAltBg", "zebra row background", func(c *ThemeColors) *string { return &c.TableRowAltBg }, "tableBorder"},
	{"statusBarBg", "status bar background", func(c *ThemeColors) *string { return &c.StatusBarBg }, "highlight"},
	{"statusBarText", "status bar text", func(c *ThemeColors) *string { return &c.StatusBarText }, "highlightText"},
	{"hintKey", "shortcut key in status bar", func(c *ThemeColors) *string { return &c.HintKey }, "statusBarText"},
	{"hintText", "shortcut label in status bar", func(c *ThemeColors) *string { return &c.HintText }, "statusBarText"},
	{"error", "errors", func(c *ThemeColors) *string { return &c.Error }, ""},
	{"warning", "warnings", func(c *ThemeColors) *string { return &c.Warning }, ""},
	{"success", "success messages", func(c *ThemeColors) *string { return &c.Success }, "accent"},
	{"info", "informational messages", func(c *ThemeColors) *string { return &c.Info }, "muted"},
}

// roleIndex returns the index of the role with the given name (matched
// case-insensitively), or -1 when no such role exists.
func roleIndex(name string) int {
	for i, r := range Roles {
		if strings.EqualFold(r.Name, name) {
			return i
		}
	}
	return -1
}

// roleCache memoizes resolved role colors for the active theme. Resolution
// walks the fallback chain with a linear registry scan per hop and is invoked
// for every styled element on every rendered frame, so the result is cached
// until the active theme or one of its colors changes (invalidateRoleCache).
var roleCache = struct {
	mu     sync.Mutex
	theme  int
	values map[string]string
}{theme: -1}

// invalidateRoleCache drops the memoized role colors. It must be called after
// any mutation of Themes[*].Colors or of the active theme index.
func invalidateRoleCache() {
	roleCache.mu.Lock()
	roleCache.values = nil
	roleCache.theme = -1
	roleCache.mu.Unlock()
}

// ResolveRole returns the effective color for a role in the active theme,
// walking the fallback chain until a non-empty value is found. An empty
// result means "terminal default".
func ResolveRole(name string) string {
	idx := getActiveTheme()
	roleCache.mu.Lock()
	if roleCache.values == nil || roleCache.theme != idx {
		roleCache.values = make(map[string]string, len(Roles))
		roleCache.theme = idx
	}
	if v, ok := roleCache.values[name]; ok {
		roleCache.mu.Unlock()
		return v
	}
	roleCache.mu.Unlock()

	v := ResolveRoleAt(idx, name)

	roleCache.mu.Lock()
	// Store only if the cache still belongs to the same theme (it may have
	// been invalidated or switched while we resolved).
	if roleCache.values != nil && roleCache.theme == idx {
		roleCache.values[name] = v
	}
	roleCache.mu.Unlock()
	return v
}

// ResolveRoleAt resolves a role's effective color for an arbitrary theme index
// (not just the active theme), walking the fallback chain. Used by the
// settings panel to preview "auto" values.
func ResolveRoleAt(themeIdx int, name string) string {
	c := Themes[themeIdx].Colors
	for hops := 0; name != "" && hops <= len(Roles); hops++ {
		i := roleIndex(name)
		if i < 0 {
			return ""
		}
		if v := *Roles[i].Ptr(&c); v != "" {
			return v
		}
		name = Roles[i].Fallback
	}
	return ""
}

// themesYAML holds the built-in theme palettes. It is the single source of
// truth for default colors — there are deliberately no color literals in the
// Go source. The user's config.yaml still overrides anything via InitFromConfig.
//
//go:embed themes.yaml
var themesYAML []byte

// Themes is the list of built-in themes (named after Australian birds), loaded
// from the embedded themes.yaml at startup. Order is significant — callers and
// tests refer to themes by index — and is preserved from the file.
//
// Any role can be overridden per-theme via config.yaml ui.themes.<name>.
var Themes = mustLoadBuiltinThemes()

func mustLoadBuiltinThemes() []Theme {
	// Each entry is a flat map of role name (plus "name") to value, so adding
	// a role to the registry automatically makes it loadable from the file.
	var defs []map[string]string
	if err := yaml.Unmarshal(themesYAML, &defs); err != nil {
		// The file is embedded at compile time, so a parse failure is a
		// programming error in themes.yaml, not a runtime/user condition.
		panic(fmt.Sprintf("ui: invalid embedded themes.yaml: %v", err))
	}
	themes := make([]Theme, len(defs))
	for i, d := range defs {
		t := Theme{Name: d["name"]}
		if t.Name == "" {
			panic(fmt.Sprintf("ui: themes.yaml entry %d has no name", i))
		}
		for key, val := range d {
			if key == "name" || val == "" {
				continue
			}
			ri := roleIndex(key)
			if ri < 0 {
				panic(fmt.Sprintf("ui: themes.yaml theme %q has unknown role %q", t.Name, key))
			}
			*Roles[ri].Ptr(&t.Colors) = val
		}
		themes[i] = t
	}
	return themes
}

// activeThemeIdx holds the index into Themes of the currently active theme.
var activeThemeIdx atomic.Int32

// SetActiveTheme atomically sets the active theme index.
func SetActiveTheme(idx int) {
	activeThemeIdx.Store(int32(idx))
	invalidateRoleCache()
}

func getActiveTheme() int {
	return int(activeThemeIdx.Load())
}

// InitFromConfig applies the UI config to the theme system: sets the active
// theme and merges any per-theme color overrides from the config file.
func InitFromConfig(ui config.UIConfig) {
	// Apply per-theme color overrides from config before setting active theme.
	// Role keys are matched case-insensitively because viper lower-cases all
	// config keys.
	for name, overrides := range ui.Themes {
		idx, ok := LookupTheme(name)
		if !ok {
			continue
		}
		c := &Themes[idx].Colors
		for key, val := range overrides {
			if val == "" {
				continue
			}
			if ri := roleIndex(key); ri >= 0 {
				*Roles[ri].Ptr(c) = val
			}
		}
	}
	invalidateRoleCache()

	if ui.Theme != "" {
		if idx, ok := LookupTheme(ui.Theme); ok {
			SetActiveTheme(idx)
			return
		}
	}
}

// ThemeNames returns the names of all available themes.
func ThemeNames() []string {
	names := make([]string, len(Themes))
	for i, t := range Themes {
		names[i] = t.Name
	}
	return names
}

// LookupTheme finds a theme index by name. Matching is case-insensitive and
// treats spaces and hyphens as equivalent.
func LookupTheme(name string) (int, bool) {
	want := normalizeThemeName(name)
	for i, t := range Themes {
		if normalizeThemeName(t.Name) == want {
			return i, true
		}
	}
	return 0, false
}

func normalizeThemeName(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "-")
}

// ActiveThemeColors returns a copy of the active theme's color palette.
func ActiveThemeColors() ThemeColors {
	return Themes[getActiveTheme()].Colors
}

// Named color accessors — use these instead of FeatherColor in new code. Each
// resolves through the role registry, so the fallback chains declared in Roles
// apply automatically.

func ColorHeading() string       { return ResolveRole("heading") }
func ColorText() string          { return ResolveRole("text") }
func ColorBackground() string    { return ResolveRole("background") }
func ColorBorder() string        { return ResolveRole("border") }
func ColorHighlight() string     { return ResolveRole("highlight") }
func ColorHighlightText() string { return ResolveRole("highlightText") }
func ColorMuted() string         { return ResolveRole("muted") }
func ColorError() string         { return ResolveRole("error") }
func ColorWarning() string       { return ResolveRole("warning") }
func ColorSuccess() string       { return ResolveRole("success") }
func ColorInfo() string          { return ResolveRole("info") }

func ColorBorderFocus() string       { return ResolveRole("borderFocus") }
func ColorAccent() string            { return ResolveRole("accent") }
func ColorTableHeader() string       { return ResolveRole("tableHeader") }
func ColorTableHeaderBg() string     { return ResolveRole("tableHeaderBg") }
func ColorTableHeaderLine() string   { return ResolveRole("tableHeaderLine") }
func ColorTableText() string         { return ResolveRole("tableText") }
func ColorTableBorder() string       { return ResolveRole("tableBorder") }
func ColorTableSelectedBg() string   { return ResolveRole("tableSelectedBg") }
func ColorTableSelectedText() string { return ResolveRole("tableSelectedText") }
func ColorTableRowAltBg() string     { return ResolveRole("tableRowAltBg") }
func ColorStatusBarBg() string       { return ResolveRole("statusBarBg") }
func ColorStatusBarText() string     { return ResolveRole("statusBarText") }
func ColorHintKey() string           { return ResolveRole("hintKey") }
func ColorHintText() string          { return ResolveRole("hintText") }

// FeatherColor returns theme colors by shade index for backwards compatibility.
// shade 0 → Heading (primary), shade 1 → Border (secondary).
func FeatherColor(shade int) string {
	c := Themes[getActiveTheme()].Colors
	if shade%2 == 0 {
		return c.Heading
	}
	return c.Border
}

// FeatherRail renders a decorative horizontal separator using the heading and
// accent colors.
func FeatherRail(width int) string {
	if width < 1 {
		return ""
	}
	colors := []string{ColorHeading(), ColorAccent()}
	styles := []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color(colors[0])),
		lipgloss.NewStyle().Foreground(lipgloss.Color(colors[1])),
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		b.WriteString(styles[i%2].Render("━"))
	}
	return b.String()
}

// ── Style helpers ─────────────────────────────────────────────────────────────

func AppStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Margin(1, 2).
		Foreground(lipgloss.Color(ColorText()))
}

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorHeading())).
		Bold(true).
		Padding(0, 1).
		MarginBottom(1)
}

func PanelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorder())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(0, 1)
}

func SelectedPanelStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText()))
}

func PanelTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorMuted())).
		Bold(true)
}

func BadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorHeading())).
		Bold(true).
		Padding(0, 1)
}

func MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
}

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorError()))
}

func SuccessStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorSuccess()))
}

func InfoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorInfo()))
}

func LoadingBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorder())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(1, 3).
		Align(lipgloss.Center)
}

func BoldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(ColorHeading()))
}

func ModalStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width+2).
		MaxHeight(height+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorderFocus())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(1, 2)
}

// StatusBarStyle returns a full-width bar with the status bar colors.
func StatusBarStyle(width int) lipgloss.Style {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().
		Width(width).
		MaxWidth(width+2).
		Background(lipgloss.Color(ColorStatusBarBg())).
		Foreground(lipgloss.Color(ColorStatusBarText())).
		Padding(0, 1)
}

// FixedPanelStyle returns a rounded-border panel locked to exact inner dimensions.
func FixedPanelStyle(width, height int) lipgloss.Style {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		MaxWidth(width+2).
		MaxHeight(height+2).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorBorder())).
		Foreground(lipgloss.Color(ColorText())).
		Padding(0, 1)
}
