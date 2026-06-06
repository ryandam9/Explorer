package ui

import (
	"strings"
	"testing"
)

// All built-in theme names must be hyphenated single tokens (no spaces), so
// they work as unquoted shell arguments to --theme.
func TestThemeNamesHaveNoSpaces(t *testing.T) {
	for _, name := range ThemeNames() {
		if strings.Contains(name, " ") {
			t.Errorf("theme name %q contains a space; use hyphens instead", name)
		}
	}
}

func TestLookupThemeMatching(t *testing.T) {
	cases := []struct {
		in      string
		wantIdx int
		wantOK  bool
	}{
		{"spotted-pardalote", 0, true},
		{"SPOTTED-PARDALOTE", 0, true}, // case-insensitive
		{"spotted pardalote", 0, true}, // legacy space-separated name still resolves
		{"  bee-eater  ", 2, true},     // surrounding whitespace ignored
		{"not-a-bird", 0, false},
	}
	for _, c := range cases {
		idx, ok := LookupTheme(c.in)
		if ok != c.wantOK || (ok && idx != c.wantIdx) {
			t.Errorf("LookupTheme(%q) = (%d, %v), want (%d, %v)", c.in, idx, ok, c.wantIdx, c.wantOK)
		}
	}
}

// Every built-in theme must fully populate all granular color roles (except
// Background, which is intentionally empty to use the terminal default). This
// guards against a new theme being added with a half-filled palette.
func TestBuiltinThemesFullyPopulated(t *testing.T) {
	for _, th := range Themes {
		c := th.Colors
		roles := map[string]string{
			"Heading":         c.Heading,
			"Text":            c.Text,
			"Border":          c.Border,
			"BorderFocus":     c.BorderFocus,
			"Highlight":       c.Highlight,
			"HighlightText":   c.HighlightText,
			"Muted":           c.Muted,
			"TableHeader":     c.TableHeader,
			"TableHeaderLine": c.TableHeaderLine,
			"StatusBarBg":     c.StatusBarBg,
			"StatusBarText":   c.StatusBarText,
			"Accent":          c.Accent,
			"Error":           c.Error,
			"Warning":         c.Warning,
		}
		for role, val := range roles {
			if val == "" {
				t.Errorf("theme %q has empty %s", th.Name, role)
			}
		}
	}
}

// Granular accessors must fall back to their related role when the underlying
// value is unset, so configs that only specify the original roles still work.
func TestGranularRoleFallbacks(t *testing.T) {
	// Build a theme with only the base roles set, append it, and activate it.
	base := ThemeColors{
		Heading: "#111111", Text: "#222222", Border: "#333333",
		Highlight: "#444444", HighlightText: "#555555", Muted: "#666666",
		Error: "#777777", Warning: "#888888",
	}
	Themes = append(Themes, Theme{Name: "fallback-probe", Colors: base})
	idx, ok := LookupTheme("fallback-probe")
	if !ok {
		t.Fatal("probe theme not found")
	}
	prev := getActiveTheme()
	SetActiveTheme(idx)
	defer func() {
		SetActiveTheme(prev)
		Themes = Themes[:len(Themes)-1]
	}()

	cases := []struct {
		name     string
		got      string
		wantSame string
	}{
		{"BorderFocus->Heading", ColorBorderFocus(), base.Heading},
		{"TableHeader->Muted", ColorTableHeader(), base.Muted},
		{"TableHeaderLine->Border", ColorTableHeaderLine(), base.Border},
		{"StatusBarBg->Highlight", ColorStatusBarBg(), base.Highlight},
		{"StatusBarText->HighlightText", ColorStatusBarText(), base.HighlightText},
		{"Accent->Heading", ColorAccent(), base.Heading},
	}
	for _, c := range cases {
		if c.got != c.wantSame {
			t.Errorf("%s = %q, want fallback %q", c.name, c.got, c.wantSame)
		}
	}
}
