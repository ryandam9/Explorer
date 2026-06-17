package docsgen

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

func TestParseHex(t *testing.T) {
	cases := []struct {
		in      string
		r, g, b int
	}{
		{"#ffffff", 255, 255, 255},
		{"#000000", 0, 0, 0},
		{"6eb245", 0x6e, 0xb2, 0x45}, // no leading '#'
		{"#fff", 255, 255, 255},      // shorthand expands
		{"#abc", 0xaa, 0xbb, 0xcc},
		{"", 0, 0, 0},        // unparseable -> black, never panics
		{"#zzzzzz", 0, 0, 0}, // invalid hex -> black
	}
	for _, c := range cases {
		r, g, b := parseHex(c.in)
		if r != c.r || g != c.g || b != c.b {
			t.Errorf("parseHex(%q) = %d,%d,%d, want %d,%d,%d", c.in, r, g, b, c.r, c.g, c.b)
		}
	}
}

func TestMixHex(t *testing.T) {
	if got := mixHex("#000000", "#ffffff", 0); got != "#000000" {
		t.Errorf("t=0 should return the first color, got %s", got)
	}
	if got := mixHex("#000000", "#ffffff", 1); got != "#ffffff" {
		t.Errorf("t=1 should return the second color, got %s", got)
	}
	if got := mixHex("#000000", "#ffffff", 0.5); got != "#808080" {
		t.Errorf("midpoint of black and white = %s, want #808080", got)
	}
}

func TestTitleizeTheme(t *testing.T) {
	if got := titleizeTheme("rose-crowned-fruit-dove"); got != "Rose Crowned Fruit Dove" {
		t.Errorf("got %q", got)
	}
	if got := titleizeTheme("galah"); got != "Galah" {
		t.Errorf("got %q", got)
	}
}

func TestThemeStyles(t *testing.T) {
	css, options := themeStyles()

	// One option per built-in theme, default included.
	if len(options) != len(ui.Themes) {
		t.Fatalf("got %d theme options, want %d", len(options), len(ui.Themes))
	}
	// A :root default and a data-theme block per theme are emitted.
	if !strings.Contains(css, ":root {") {
		t.Error("theme CSS missing :root default block")
	}
	for _, name := range ui.ThemeNames() {
		if !strings.Contains(css, "[data-theme=\""+name+"\"]") {
			t.Errorf("theme CSS missing block for %q", name)
		}
	}
	// The default theme's heading color appears (palette is wired through).
	if idx, ok := ui.LookupTheme(defaultDocTheme); ok {
		if h := ui.ResolveRoleAt(idx, "heading"); h != "" && !strings.Contains(css, h) {
			t.Errorf("theme CSS missing default heading color %q", h)
		}
	}
}
