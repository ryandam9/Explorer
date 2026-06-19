package ui

import (
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
)

// Highlighting must be lossless: stripping the ANSI styling recovers the exact
// source, so it never corrupts what the user reads.
func TestHighlightPreservesContent(t *testing.T) {
	code := "package main\n\nfunc main() {\n\t_ = \"hello\" // hi\n}\n"
	if got := ansi.Strip(Highlight(code, "main.go")); got != code {
		t.Errorf("content changed:\n got %q\nwant %q", got, code)
	}
}

func TestHighlightUnknownLanguagePassthrough(t *testing.T) {
	if got := HighlightLang("a b c", "definitely-not-a-language"); got != "a b c" {
		t.Errorf("unknown language should pass through, got %q", got)
	}
	// An extension with no lexer (and no analysable content) is returned as-is.
	if got := Highlight("xyzzy", "notes.unknownext"); ansi.Strip(got) != "xyzzy" {
		t.Errorf("unknown file content changed: %q", ansi.Strip(got))
	}
}

func TestHighlightEmpty(t *testing.T) {
	if got := Highlight("", "x.go"); got != "" {
		t.Errorf("empty input = %q", got)
	}
}

// tokenColor maps token categories to the active theme's roles, so highlighting
// follows the theme and stays consistent with the rest of the UI.
func TestTokenColorMapping(t *testing.T) {
	SetActiveTheme(0)
	cases := map[chroma.TokenType]string{
		chroma.Comment:       ColorMuted(),
		chroma.CommentSingle: ColorMuted(),
		chroma.LiteralString: ColorSuccess(),
		chroma.LiteralNumber: ColorWarning(),
		chroma.Keyword:       ColorAccent(),
		chroma.KeywordType:   ColorInfo(),
		chroma.NameFunction:  ColorHeading(),
	}
	for tt, want := range cases {
		if got := tokenColor(tt); got != want {
			t.Errorf("tokenColor(%v) = %q, want %q", tt, got, want)
		}
	}
	// Operators and punctuation keep the terminal default (no forced colour).
	if got := tokenColor(chroma.Punctuation); got != "" {
		t.Errorf("punctuation should be uncoloured, got %q", got)
	}
}
