package tui

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
