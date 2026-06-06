package ui

import (
	_ "embed"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"go.yaml.in/yaml/v3"

	"github.com/user/aws_explorer/internal/config"
)

// ThemeColors holds the full color palette for a theme.
//
// Colors are taken from the "feathers" palettes (color schemes inspired by the
// plumage of Australian birds — https://github.com/shandiya/feathers, the same
// data rendered at https://ryandam.net/demos/feathers_palettes/). Each role is
// granular so editing one UI area never bleeds into another. Roles past the
// first nine were added so areas that previously shared a single color (table
// headers, focused borders, the status bar, decorative accents) can each be
// themed independently.
type ThemeColors struct {
	Heading         string // titles, section headers
	Text            string // body text / foreground
	Background      string // panel background (empty = terminal default)
	Border          string // borders of unfocused panels
	BorderFocus     string // border of the focused panel
	Highlight       string // selected row background
	HighlightText   string // text on the selected row
	Muted           string // de-emphasised / secondary text
	TableHeader     string // table column header text
	TableHeaderLine string // rule drawn under table headers
	StatusBarBg     string // status bar background
	StatusBarText   string // status bar text
	Accent          string // decorative rails, input prompts and cursors
	Error           string // error messages and indicators
	Warning         string // warning messages and indicators
}

// Theme holds a named theme with its full color palette.
type Theme struct {
	Name   string
	Colors ThemeColors
}

// themesYAML holds the built-in theme palettes. It is the single source of
// truth for default colors — there are deliberately no color literals in the
// Go source. The user's config.yaml still overrides anything via InitFromConfig.
//
//go:embed themes.yaml
var themesYAML []byte

// themeDef mirrors one entry in themes.yaml. Keeping a dedicated type (rather
// than reusing config.ThemeColorConfig) keeps the embedded-defaults format
// decoupled from the user-facing config schema.
type themeDef struct {
	Name            string `yaml:"name"`
	Heading         string `yaml:"heading"`
	Text            string `yaml:"text"`
	Background      string `yaml:"background"`
	Border          string `yaml:"border"`
	BorderFocus     string `yaml:"borderFocus"`
	Highlight       string `yaml:"highlight"`
	HighlightText   string `yaml:"highlightText"`
	Muted           string `yaml:"muted"`
	TableHeader     string `yaml:"tableHeader"`
	TableHeaderLine string `yaml:"tableHeaderLine"`
	StatusBarBg     string `yaml:"statusBarBg"`
	StatusBarText   string `yaml:"statusBarText"`
	Accent          string `yaml:"accent"`
	Error           string `yaml:"error"`
	Warning         string `yaml:"warning"`
}

// Themes is the list of built-in themes (named after Australian birds), loaded
// from the embedded themes.yaml at startup. Order is significant — callers and
// tests refer to themes by index — and is preserved from the file.
//
// Any role can be overridden per-theme via config.yaml ui.themes.<name>.
var Themes = mustLoadBuiltinThemes()

func mustLoadBuiltinThemes() []Theme {
	var defs []themeDef
	if err := yaml.Unmarshal(themesYAML, &defs); err != nil {
		// The file is embedded at compile time, so a parse failure is a
		// programming error in themes.yaml, not a runtime/user condition.
		panic(fmt.Sprintf("ui: invalid embedded themes.yaml: %v", err))
	}
	themes := make([]Theme, len(defs))
	for i, d := range defs {
		themes[i] = Theme{
			Name: d.Name,
			Colors: ThemeColors{
				Heading: d.Heading, Text: d.Text, Background: d.Background,
				Border: d.Border, BorderFocus: d.BorderFocus,
				Highlight: d.Highlight, HighlightText: d.HighlightText, Muted: d.Muted,
				TableHeader: d.TableHeader, TableHeaderLine: d.TableHeaderLine,
				StatusBarBg: d.StatusBarBg, StatusBarText: d.StatusBarText, Accent: d.Accent,
				Error: d.Error, Warning: d.Warning,
			},
		}
	}
	return themes
}

// activeThemeIdx holds the index into Themes of the currently active theme.
var activeThemeIdx atomic.Int32

// SetActiveTheme atomically sets the active theme index.
func SetActiveTheme(idx int) {
	activeThemeIdx.Store(int32(idx))
}

func getActiveTheme() int {
	return int(activeThemeIdx.Load())
}

// InitFromConfig applies the UI config to the theme system: sets the active
// theme and merges any per-theme color overrides from the config file.
func InitFromConfig(ui config.UIConfig) {
	// Apply per-theme color overrides from config before setting active theme.
	for name, cfg := range ui.Themes {
		idx, ok := LookupTheme(name)
		if !ok {
			continue
		}
		c := &Themes[idx].Colors
		if cfg.Heading != "" {
			c.Heading = cfg.Heading
		}
		if cfg.Text != "" {
			c.Text = cfg.Text
		}
		if cfg.Background != "" {
			c.Background = cfg.Background
		}
		if cfg.Border != "" {
			c.Border = cfg.Border
		}
		if cfg.BorderFocus != "" {
			c.BorderFocus = cfg.BorderFocus
		}
		if cfg.Highlight != "" {
			c.Highlight = cfg.Highlight
		}
		if cfg.HighlightText != "" {
			c.HighlightText = cfg.HighlightText
		}
		if cfg.Muted != "" {
			c.Muted = cfg.Muted
		}
		if cfg.TableHeader != "" {
			c.TableHeader = cfg.TableHeader
		}
		if cfg.TableHeaderLine != "" {
			c.TableHeaderLine = cfg.TableHeaderLine
		}
		if cfg.StatusBarBg != "" {
			c.StatusBarBg = cfg.StatusBarBg
		}
		if cfg.StatusBarText != "" {
			c.StatusBarText = cfg.StatusBarText
		}
		if cfg.Accent != "" {
			c.Accent = cfg.Accent
		}
		if cfg.Error != "" {
			c.Error = cfg.Error
		}
		if cfg.Warning != "" {
			c.Warning = cfg.Warning
		}
	}

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

// Named color accessors — use these instead of FeatherColor in new code.

func ColorHeading() string       { return Themes[getActiveTheme()].Colors.Heading }
func ColorText() string          { return Themes[getActiveTheme()].Colors.Text }
func ColorBackground() string    { return Themes[getActiveTheme()].Colors.Background }
func ColorBorder() string        { return Themes[getActiveTheme()].Colors.Border }
func ColorHighlight() string     { return Themes[getActiveTheme()].Colors.Highlight }
func ColorHighlightText() string { return Themes[getActiveTheme()].Colors.HighlightText }
func ColorMuted() string         { return Themes[getActiveTheme()].Colors.Muted }
func ColorError() string         { return Themes[getActiveTheme()].Colors.Error }
func ColorWarning() string       { return Themes[getActiveTheme()].Colors.Warning }

// Granular accessors. Each falls back to a related role when its own value is
// unset, so configs that only specify the original nine roles keep working and
// users can override just the knobs they care about.

func ColorBorderFocus() string {
	if c := Themes[getActiveTheme()].Colors.BorderFocus; c != "" {
		return c
	}
	return ColorHeading()
}

func ColorTableHeader() string {
	if c := Themes[getActiveTheme()].Colors.TableHeader; c != "" {
		return c
	}
	return ColorMuted()
}

func ColorTableHeaderLine() string {
	if c := Themes[getActiveTheme()].Colors.TableHeaderLine; c != "" {
		return c
	}
	return ColorBorder()
}

func ColorStatusBarBg() string {
	if c := Themes[getActiveTheme()].Colors.StatusBarBg; c != "" {
		return c
	}
	return ColorHighlight()
}

func ColorStatusBarText() string {
	if c := Themes[getActiveTheme()].Colors.StatusBarText; c != "" {
		return c
	}
	return ColorHighlightText()
}

func ColorAccent() string {
	if c := Themes[getActiveTheme()].Colors.Accent; c != "" {
		return c
	}
	return ColorHeading()
}

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

func InfoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
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

// StatusBarStyle returns a full-width bar with highlight background and text.
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
