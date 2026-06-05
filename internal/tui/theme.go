package tui

import (
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/user/aws_explorer/internal/config"
)

// ThemeColors holds the full color palette for a theme.
type ThemeColors struct {
	Heading       string // titles, section headers
	Text          string // body text / foreground
	Background    string // panel background (empty = terminal default)
	Border        string // panel borders
	Highlight     string // selected / highlighted item background
	HighlightText string // text on selected / highlighted items
	Muted         string // de-emphasised / secondary text
	Error         string // error messages and indicators
	Warning       string // warning messages and indicators
}

// Theme holds a named theme with its full color palette.
type Theme struct {
	Name   string
	Colors ThemeColors
}

// Themes is the list of built-in themes (named after Australian birds).
// Color roles beyond Heading/Border are derived from the two-tone originals
// and can be overridden via config.yaml ui.themes.<name>.
var Themes = []Theme{
	{
		Name: "spotted-pardalote",
		Colors: ThemeColors{
			Heading: "#6260FF", Text: "#E4E4FF", Background: "",
			Border: "#E4E4FF", Highlight: "#6260FF", HighlightText: "#E4E4FF",
			Muted: "#E4E4FF", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "plains-wanderer",
		Colors: ThemeColors{
			Heading: "#9FE870", Text: "#9FE870", Background: "",
			Border: "#163300", Highlight: "#9FE870", HighlightText: "#163300",
			Muted: "#163300", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "bee-eater",
		Colors: ThemeColors{
			Heading: "#BDD9D7", Text: "#BDD9D7", Background: "",
			Border: "#03363D", Highlight: "#BDD9D7", HighlightText: "#03363D",
			Muted: "#03363D", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "rose-crowned-fruit-dove",
		Colors: ThemeColors{
			Heading: "#3447AA", Text: "#FBEAEB", Background: "",
			Border: "#FBEAEB", Highlight: "#3447AA", HighlightText: "#FBEAEB",
			Muted: "#FBEAEB", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "eastern-rosella",
		Colors: ThemeColors{
			Heading: "#FCDB32", Text: "#FCDB32", Background: "",
			Border: "#141D38", Highlight: "#FCDB32", HighlightText: "#141D38",
			Muted: "#141D38", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "oriole",
		Colors: ThemeColors{
			Heading: "#34E0A1", Text: "#34E0A1", Background: "",
			Border: "#000000", Highlight: "#34E0A1", HighlightText: "#000000",
			Muted: "#888888", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "princess-parrot",
		Colors: ThemeColors{
			Heading: "#FF69B4", Text: "#FF69B4", Background: "",
			Border: "#006400", Highlight: "#FF69B4", HighlightText: "#006400",
			Muted: "#006400", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "superb-fairy-wren",
		Colors: ThemeColors{
			Heading: "#1E90FF", Text: "#1E90FF", Background: "",
			Border: "#8B4513", Highlight: "#1E90FF", HighlightText: "#8B4513",
			Muted: "#8B4513", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "cassowary",
		Colors: ThemeColors{
			Heading: "#191970", Text: "#DC143C", Background: "",
			Border: "#DC143C", Highlight: "#191970", HighlightText: "#DC143C",
			Muted: "#DC143C", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "yellow-robin",
		Colors: ThemeColors{
			Heading: "#FFD700", Text: "#FFD700", Background: "",
			Border: "#696969", Highlight: "#FFD700", HighlightText: "#696969",
			Muted: "#696969", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "galah",
		Colors: ThemeColors{
			Heading: "#FF69B4", Text: "#FF69B4", Background: "",
			Border: "#808080", Highlight: "#FF69B4", HighlightText: "#808080",
			Muted: "#808080", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
	{
		Name: "blue-winged-kookaburra",
		Colors: ThemeColors{
			Heading: "#4169E1", Text: "#4169E1", Background: "",
			Border: "#D2691E", Highlight: "#4169E1", HighlightText: "#D2691E",
			Muted: "#D2691E", Error: "#FF5555", Warning: "#FFAA00",
		},
	},
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
		if cfg.Highlight != "" {
			c.Highlight = cfg.Highlight
		}
		if cfg.HighlightText != "" {
			c.HighlightText = cfg.HighlightText
		}
		if cfg.Muted != "" {
			c.Muted = cfg.Muted
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

// FeatherColor returns theme colors by shade index for backwards compatibility.
// shade 0 → Heading (primary), shade 1 → Border (secondary).
func FeatherColor(shade int) string {
	c := Themes[getActiveTheme()].Colors
	if shade%2 == 0 {
		return c.Heading
	}
	return c.Border
}

// FeatherRail renders a decorative horizontal separator using heading/border colors.
func FeatherRail(width int) string {
	if width < 1 {
		return ""
	}
	colors := []string{ColorHeading(), ColorBorder()}
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
		BorderForeground(lipgloss.Color(ColorHeading())).
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
		BorderForeground(lipgloss.Color(ColorHeading())).
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
		Background(lipgloss.Color(ColorHighlight())).
		Foreground(lipgloss.Color(ColorHighlightText())).
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
