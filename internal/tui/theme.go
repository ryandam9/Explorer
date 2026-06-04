package tui

import (
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds a named pair of display colors.
type Theme struct {
	Name   string
	Colors []string // [primary, secondary]
}

// Themes is the list of built-in themes (named after Australian birds).
var Themes = []Theme{
	{Name: "spotted pardalote", Colors: []string{"#6260FF", "#E4E4FF"}},
	{Name: "plains wanderer", Colors: []string{"#9FE870", "#163300"}},
	{Name: "bee-eater", Colors: []string{"#BDD9D7", "#03363D"}},
	{Name: "rose-crowned fruit dove", Colors: []string{"#3447AA", "#FBEAEB"}},
	{Name: "eastern rosella", Colors: []string{"#FCDB32", "#141D38"}},
	{Name: "oriole", Colors: []string{"#34E0A1", "#000000"}},
	{Name: "princess parrot", Colors: []string{"#FF69B4", "#006400"}},
	{Name: "superb fairy-wren", Colors: []string{"#1E90FF", "#8B4513"}},
	{Name: "cassowary", Colors: []string{"#191970", "#DC143C"}},
	{Name: "yellow robin", Colors: []string{"#FFD700", "#696969"}},
	{Name: "galah", Colors: []string{"#FF69B4", "#808080"}},
	{Name: "blue-winged kookaburra", Colors: []string{"#4169E1", "#D2691E"}},
}

// activeThemeIdx holds the index into Themes of the currently active theme.
// All reads and writes go through SetActiveTheme / getActiveTheme.
var activeThemeIdx atomic.Int32

// SetActiveTheme atomically sets the active theme index.
func SetActiveTheme(idx int) {
	activeThemeIdx.Store(int32(idx))
}

func getActiveTheme() int {
	return int(activeThemeIdx.Load())
}

// ThemeNames returns the names of all available themes.
func ThemeNames() []string {
	names := make([]string, len(Themes))
	for i, t := range Themes {
		names[i] = t.Name
	}
	return names
}

// LookupTheme finds a theme index by name (case-insensitive).
func LookupTheme(name string) (int, bool) {
	for i, t := range Themes {
		if strings.EqualFold(t.Name, name) {
			return i, true
		}
	}
	return 0, false
}

// FeatherColor returns a color from the active theme by shade index (0=primary, 1=secondary).
func FeatherColor(shade int) string {
	colors := Themes[getActiveTheme()].Colors
	return colors[shade%len(colors)]
}

// FeatherRail renders a decorative horizontal separator using the active theme colors.
func FeatherRail(width int) string {
	if width < 1 {
		return ""
	}
	colors := Themes[getActiveTheme()].Colors
	// Build one style per color (typically 2) and reuse them across the full width.
	styles := make([]lipgloss.Style, len(colors))
	for i, c := range colors {
		styles[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	var b strings.Builder
	for i := 0; i < width; i++ {
		b.WriteString(styles[i%len(styles)].Render("━"))
	}
	return b.String()
}

func AppStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Margin(1, 2).
		Foreground(lipgloss.Color(FeatherColor(0)))
}

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(FeatherColor(0))).
		Bold(true).
		Padding(0, 1).
		MarginBottom(1)
}

func PanelStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(FeatherColor(1))).
		Foreground(lipgloss.Color(FeatherColor(0))).
		Padding(0, 1)
}

func SelectedPanelStyle() lipgloss.Style {
	return PanelStyle().
		BorderForeground(lipgloss.Color(FeatherColor(1))).
		Foreground(lipgloss.Color(FeatherColor(0)))
}

func PanelTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(FeatherColor(1))).
		Bold(true)
}

func BadgeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(FeatherColor(0))).
		Bold(true).
		Padding(0, 1)
}

func MutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(FeatherColor(1)))
}

func ErrorStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(FeatherColor(0)))
}

func InfoStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(FeatherColor(1)))
}

func LoadingBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(FeatherColor(1))).
		Foreground(lipgloss.Color(FeatherColor(0))).
		Padding(1, 3).
		Align(lipgloss.Center)
}

func BoldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(FeatherColor(0)))
}

func ModalStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(FeatherColor(0))).
		Foreground(lipgloss.Color(FeatherColor(0))).
		Padding(1, 2)
}
