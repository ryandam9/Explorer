package ui

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// configUI builds a UIConfig with overrides for a single theme.
func configUI(theme string, overrides map[string]string) config.UIConfig {
	return config.UIConfig{Themes: map[string]map[string]string{theme: overrides}}
}

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
		// Granular table roles fall back to the general roles.
		{"TableText->Text", ColorTableText(), base.Text},
		{"TableBorder->Border", ColorTableBorder(), base.Border},
		{"TableSelectedBg->Highlight", ColorTableSelectedBg(), base.Highlight},
		{"TableSelectedText->HighlightText", ColorTableSelectedText(), base.HighlightText},
		// Zebra background falls back through tableBorder to the base border.
		{"TableRowAltBg->TableBorder->Border", ColorTableRowAltBg(), base.Border},
		// Multi-hop chains: hintKey -> statusBarText -> highlightText, and
		// success -> accent -> heading.
		{"HintKey->StatusBarText->HighlightText", ColorHintKey(), base.HighlightText},
		{"HintText->StatusBarText->HighlightText", ColorHintText(), base.HighlightText},
		{"Success->Accent->Heading", ColorSuccess(), base.Heading},
		{"Info->Muted", ColorInfo(), base.Muted},
	}
	for _, c := range cases {
		if c.got != c.wantSame {
			t.Errorf("%s = %q, want fallback %q", c.name, c.got, c.wantSame)
		}
	}
}

// The role registry must be internally consistent: unique names, and every
// fallback must name another registered role (no cycles back to itself).
func TestRoleRegistryConsistent(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Roles {
		key := strings.ToLower(r.Name)
		if seen[key] {
			t.Errorf("duplicate role name %q", r.Name)
		}
		seen[key] = true
		if r.Fallback != "" {
			if roleIndex(r.Fallback) < 0 {
				t.Errorf("role %q falls back to unknown role %q", r.Name, r.Fallback)
			}
			if strings.EqualFold(r.Fallback, r.Name) {
				t.Errorf("role %q falls back to itself", r.Name)
			}
		}
		// Ptr must address a distinct field per role.
		var c ThemeColors
		*r.Ptr(&c) = "probe"
		count := 0
		for _, r2 := range Roles {
			if *r2.Ptr(&c) == "probe" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("role %q shares a backing field with another role", r.Name)
		}
	}
}

// Config overrides must apply through the registry, matching role names
// case-insensitively (viper lower-cases config keys).
func TestInitFromConfigOverridesRoles(t *testing.T) {
	base := ThemeColors{Heading: "#101010", Text: "#202020"}
	Themes = append(Themes, Theme{Name: "override-probe", Colors: base})
	defer func() { Themes = Themes[:len(Themes)-1] }()

	InitFromConfig(configUI("override-probe", map[string]string{
		"tableheaderbg":     "#aaaaaa", // lower-cased, as viper delivers it
		"tableSelectedText": "#bbbbbb",
		"tablerowaltbg":     "#cccccc", // the zebra row colour, set from config
		"notARole":          "#ffffff", // silently ignored
	}))

	idx, _ := LookupTheme("override-probe")
	c := Themes[idx].Colors
	if c.TableHeaderBg != "#aaaaaa" {
		t.Errorf("TableHeaderBg = %q, want #aaaaaa", c.TableHeaderBg)
	}
	if c.TableSelectedText != "#bbbbbb" {
		t.Errorf("TableSelectedText = %q, want #bbbbbb", c.TableSelectedText)
	}
	if c.TableRowAltBg != "#cccccc" {
		t.Errorf("TableRowAltBg = %q, want #cccccc", c.TableRowAltBg)
	}
	if c.Heading != "#101010" {
		t.Errorf("Heading = %q, want untouched #101010", c.Heading)
	}
}

func TestResolveRoleCacheInvalidatedOnEdits(t *testing.T) {
	// Work on a non-default theme index and restore everything afterwards so
	// other tests see pristine globals.
	const themeIdx = 1
	ri := roleIndex("heading")
	if ri < 0 {
		t.Fatal("heading role missing")
	}
	origActive := getActiveTheme()
	origVal := *Roles[ri].Ptr(&Themes[themeIdx].Colors)
	t.Cleanup(func() {
		setColorForField(themeIdx, ri, origVal)
		SetActiveTheme(origActive)
	})

	SetActiveTheme(themeIdx)
	before := ResolveRole("heading")
	if before == "" {
		t.Fatal("expected a heading color")
	}
	// Resolve again (now served from the cache), then mutate the role the way
	// the settings panel does and check the cache was invalidated.
	if again := ResolveRole("heading"); again != before {
		t.Fatalf("cached resolve changed value: %q vs %q", again, before)
	}
	setColorForField(themeIdx, ri, "#123456")
	if got := ResolveRole("heading"); got != "#123456" {
		t.Errorf("ResolveRole after live edit = %q, want %q (stale cache?)", got, "#123456")
	}

	// Switching themes must also drop the memoized values.
	SetActiveTheme(origActive)
	if got := ResolveRole("heading"); got == "#123456" && origActive != themeIdx {
		t.Error("ResolveRole still returns the edited theme's color after switching themes")
	}
}
