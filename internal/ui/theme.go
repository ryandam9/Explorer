package ui

import (
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
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

// errColor / warnColor are shared across all built-in themes. The feathers
// palettes don't include dedicated error/warning hues, so we use conventional
// terminal red/amber that read clearly on any background.
const (
	errColor  = "#FF5555"
	warnColor = "#FFAA00"
)

// Themes is the list of built-in themes (named after Australian birds). The
// colors come straight from the "feathers" palettes; each palette's distinct
// hues are mapped onto the granular roles below. Order is significant — callers
// and tests refer to themes by index — so keep spotted-pardalote first.
//
// Any role can be overridden per-theme via config.yaml ui.themes.<name>.
var Themes = []Theme{
	{
		// feathers: #feca00 #d36328 #cb0300 #b4b9b3 #424847 #000100
		Name: "spotted-pardalote",
		Colors: ThemeColors{
			Heading: "#feca00", Text: "#b4b9b3", Background: "",
			Border: "#424847", BorderFocus: "#feca00",
			Highlight: "#cb0300", HighlightText: "#feca00", Muted: "#d36328",
			TableHeader: "#d36328", TableHeaderLine: "#424847",
			StatusBarBg: "#feca00", StatusBarText: "#000100", Accent: "#d36328",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #edd8c5 #d09a5e #e7aa01 #ac570f #73481b #442c0e #0d0403
		Name: "plains-wanderer",
		Colors: ThemeColors{
			Heading: "#e7aa01", Text: "#edd8c5", Background: "",
			Border: "#73481b", BorderFocus: "#e7aa01",
			Highlight: "#ac570f", HighlightText: "#edd8c5", Muted: "#d09a5e",
			TableHeader: "#d09a5e", TableHeaderLine: "#442c0e",
			StatusBarBg: "#e7aa01", StatusBarText: "#442c0e", Accent: "#d09a5e",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #00346E #007CBF #06ABDF #EDD03E #F5A200 #6D8600 #424D0C
		Name: "bee-eater",
		Colors: ThemeColors{
			Heading: "#06ABDF", Text: "#EDD03E", Background: "",
			Border: "#00346E", BorderFocus: "#06ABDF",
			Highlight: "#007CBF", HighlightText: "#EDD03E", Muted: "#007CBF",
			TableHeader: "#F5A200", TableHeaderLine: "#424D0C",
			StatusBarBg: "#F5A200", StatusBarText: "#00346E", Accent: "#6D8600",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #BD338F #EB8252 #F5DC83 #CDD4DC #8098A2 #8FA33F #5F7929 #014820
		Name: "rose-crowned-fruit-dove",
		Colors: ThemeColors{
			Heading: "#BD338F", Text: "#CDD4DC", Background: "",
			Border: "#5F7929", BorderFocus: "#BD338F",
			Highlight: "#BD338F", HighlightText: "#F5DC83", Muted: "#8098A2",
			TableHeader: "#EB8252", TableHeaderLine: "#014820",
			StatusBarBg: "#EB8252", StatusBarText: "#014820", Accent: "#F5DC83",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #cd3122 #f4c623 #bee183 #6c905e #2f533c #b8c9dc #2f7ab9
		Name: "eastern-rosella",
		Colors: ThemeColors{
			Heading: "#f4c623", Text: "#bee183", Background: "",
			Border: "#2f533c", BorderFocus: "#f4c623",
			Highlight: "#cd3122", HighlightText: "#f4c623", Muted: "#b8c9dc",
			TableHeader: "#2f7ab9", TableHeaderLine: "#2f533c",
			StatusBarBg: "#f4c623", StatusBarText: "#2f533c", Accent: "#2f7ab9",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #8a3223 #bb5645 #d97878 #e2aba0 #d0cfe9 #a29eb8 #6c6b75 #b8a53f #93862a #4d4019
		Name: "oriole",
		Colors: ThemeColors{
			Heading: "#b8a53f", Text: "#e2aba0", Background: "",
			Border: "#6c6b75", BorderFocus: "#d97878",
			Highlight: "#8a3223", HighlightText: "#e2aba0", Muted: "#a29eb8",
			TableHeader: "#bb5645", TableHeaderLine: "#4d4019",
			StatusBarBg: "#b8a53f", StatusBarText: "#4d4019", Accent: "#d0cfe9",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #7090c9 #8cb3de #afbe9f #616020 #6eb245 #214917 #cf2236 #d683ad
		Name: "princess-parrot",
		Colors: ThemeColors{
			Heading: "#6eb245", Text: "#8cb3de", Background: "",
			Border: "#214917", BorderFocus: "#6eb245",
			Highlight: "#cf2236", HighlightText: "#8cb3de", Muted: "#afbe9f",
			TableHeader: "#7090c9", TableHeaderLine: "#214917",
			StatusBarBg: "#6eb245", StatusBarText: "#214917", Accent: "#d683ad",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #4F3321 #AA7853 #D9C4A7 #B03F05 #020503
		Name: "superb-fairy-wren",
		Colors: ThemeColors{
			Heading: "#B03F05", Text: "#D9C4A7", Background: "",
			Border: "#4F3321", BorderFocus: "#B03F05",
			Highlight: "#B03F05", HighlightText: "#D9C4A7", Muted: "#AA7853",
			TableHeader: "#AA7853", TableHeaderLine: "#4F3321",
			StatusBarBg: "#AA7853", StatusBarText: "#020503", Accent: "#AA7853",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #BDA14D #3EBCB6 #0169C4 #153460 #D5114E #A56EB6 #4B1C57 #09090C
		Name: "cassowary",
		Colors: ThemeColors{
			Heading: "#3EBCB6", Text: "#BDA14D", Background: "",
			Border: "#153460", BorderFocus: "#3EBCB6",
			Highlight: "#D5114E", HighlightText: "#BDA14D", Muted: "#A56EB6",
			TableHeader: "#0169C4", TableHeaderLine: "#4B1C57",
			StatusBarBg: "#3EBCB6", StatusBarText: "#09090C", Accent: "#A56EB6",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #E19E00 #FBEB5B #85773A #979EB9 #727B98 #454B56 #201B1E
		Name: "yellow-robin",
		Colors: ThemeColors{
			Heading: "#FBEB5B", Text: "#979EB9", Background: "",
			Border: "#454B56", BorderFocus: "#E19E00",
			Highlight: "#E19E00", HighlightText: "#201B1E", Muted: "#85773A",
			TableHeader: "#E19E00", TableHeaderLine: "#454B56",
			StatusBarBg: "#FBEB5B", StatusBarText: "#201B1E", Accent: "#727B98",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #FFD2CF #E9A7BB #D05478 #AAB9CC #8390A2 #4C5766
		Name: "galah",
		Colors: ThemeColors{
			Heading: "#D05478", Text: "#FFD2CF", Background: "",
			Border: "#4C5766", BorderFocus: "#D05478",
			Highlight: "#D05478", HighlightText: "#FFD2CF", Muted: "#AAB9CC",
			TableHeader: "#E9A7BB", TableHeaderLine: "#4C5766",
			StatusBarBg: "#D05478", StatusBarText: "#FFD2CF", Accent: "#8390A2",
			Error: errColor, Warning: warnColor,
		},
	},
	{
		// feathers: #b5effb #0b7595 #02407c #06213a #c45829 #9C4620 #622C14 #d4d8e3 #b8bcd8 #ad8d9f #725f77
		Name: "blue-winged-kookaburra",
		Colors: ThemeColors{
			Heading: "#b5effb", Text: "#d4d8e3", Background: "",
			Border: "#02407c", BorderFocus: "#b5effb",
			Highlight: "#0b7595", HighlightText: "#b5effb", Muted: "#b8bcd8",
			TableHeader: "#c45829", TableHeaderLine: "#06213a",
			StatusBarBg: "#c45829", StatusBarText: "#06213a", Accent: "#ad8d9f",
			Error: errColor, Warning: warnColor,
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
