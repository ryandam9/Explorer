package ui

import (
	"testing"
	"unicode"
)

// Off (the default), service icons disappear and status markers keep the
// plain symbols the UI has always used; on, every name has a private-use-area
// Nerd Font glyph.
func TestIcons(t *testing.T) {
	defer SetNerdFont(false)

	SetNerdFont(false)
	if Icon("lambda") != "" || Icon("S3") != "" {
		t.Error("service icons must be absent without a Nerd Font")
	}
	if Glyph("cursor") != "▶" || Icon("region") != "◉ " {
		t.Errorf("status markers need their plain fallback: %q %q", Glyph("cursor"), Icon("region"))
	}
	if Icon("no-such-icon") != "" {
		t.Error("unknown icons must render as nothing")
	}

	SetNerdFont(true)
	for name, spec := range icons {
		r := []rune(spec.nerd)
		if len(r) != 1 || !unicode.In(r[0], unicode.Co) {
			t.Errorf("icon %q: Nerd Font glyph %q is not a single private-use rune", name, spec.nerd)
		}
		if Icon(name) != spec.nerd+" " {
			t.Errorf("Icon(%q) = %q", name, Icon(name))
		}
	}
}
