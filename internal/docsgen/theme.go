package docsgen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// defaultDocTheme is the palette the HTML site loads with — the same default
// the TUI ships with, so the docs and the app open wearing the same skin.
const defaultDocTheme = "princess-parrot"

// docThemeOption is one selectable palette in the HTML site's theme switcher.
type docThemeOption struct {
	Name  string // theme key, e.g. "princess-parrot"
	Label string // human label, e.g. "Princess Parrot"
}

// themeStyles builds the CSS custom-property blocks for every built-in app
// theme plus the option list for the switcher. The HTML docs reuse the real
// "feathers" palettes from internal/ui, so the site wears the same colors as
// the TUI rather than a generic black-and-white sheet (issue #300).
func themeStyles() (css string, options []docThemeOption) {
	var b strings.Builder

	// :root carries the default palette so the page is fully styled even
	// before the switcher's script runs (no flash of unstyled colors).
	defIdx := 0
	if idx, ok := ui.LookupTheme(defaultDocTheme); ok {
		defIdx = idx
	}
	fmt.Fprintf(&b, ":root {\n%s}\n", themeVars(defIdx))

	for i, t := range ui.Themes {
		fmt.Fprintf(&b, "[data-theme=%q] {\n%s}\n", t.Name, themeVars(i))
		options = append(options, docThemeOption{Name: t.Name, Label: titleizeTheme(t.Name)})
	}
	return b.String(), options
}

// themeVars renders the CSS variable declarations for one theme. The vivid
// feather roles (heading, accent, status bar, table header, alerts) are used
// verbatim; the dark page/panel/code surfaces are tinted from the theme's own
// border hue so each palette reads as a distinct skin while staying readable.
func themeVars(i int) string {
	role := func(name string) string { return ui.ResolveRoleAt(i, name) }

	heading := role("heading")
	text := role("text")
	muted := role("muted")
	accent := role("accent")
	border := role("border")
	focus := role("borderFocus")

	// ink is the near-black base every dark surface is blended from; mixing in
	// a little of the theme's border tints the whole chrome toward the palette.
	const ink = "#0b0b0e"
	bg := mixHex(ink, border, 0.10)
	panel := mixHex(ink, border, 0.20)
	code := mixHex(ink, border, 0.28)
	line := mixHex(border, text, 0.30) // a rule that stays visible on the dark bg

	var b strings.Builder
	put := func(name, val string) { fmt.Fprintf(&b, "  --%s: %s;\n", name, val) }
	put("bg", bg)
	put("panel", panel)
	put("code", code)
	put("border", line)
	put("text", text)
	put("muted", muted)
	put("heading", heading)
	put("link", accent)
	put("link-hover", heading)
	put("accent", accent)
	put("focus", focus)
	put("status-bg", role("statusBarBg"))
	put("status-text", role("statusBarText"))
	put("th-text", role("tableHeader"))
	put("th-bg", mixHex(panel, focus, 0.14))
	put("th-line", role("tableHeaderLine"))
	put("error", role("error"))
	put("warning", role("warning"))
	put("success", role("success"))
	put("info", role("info"))
	return b.String()
}

// titleizeTheme turns a theme key like "rose-crowned-fruit-dove" into a display
// label like "Rose Crowned Fruit Dove".
func titleizeTheme(name string) string {
	words := strings.Split(name, "-")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// mixHex linearly blends two "#rrggbb" colors: t=0 returns a, t=1 returns b.
// Unparseable input contributes black so a bad value can never panic the docs.
func mixHex(a, b string, t float64) string {
	ar, ag, ab := parseHex(a)
	br, bg, bb := parseHex(b)
	mix := func(x, y int) int {
		v := int(float64(x)*(1-t) + float64(y)*t + 0.5)
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// parseHex decodes "#rgb" or "#rrggbb" into 8-bit channels; anything else
// returns black (0,0,0).
func parseHex(s string) (r, g, b int) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return 0, 0, 0
	}
	ch := func(hex string) int {
		v, err := strconv.ParseInt(hex, 16, 0)
		if err != nil {
			return 0
		}
		return int(v)
	}
	return ch(s[0:2]), ch(s[2:4]), ch(s[4:6])
}
