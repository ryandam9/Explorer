package ui

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
)

// Syntax highlighting — a small, reusable component for colouring source code
// and structured data (JSON, YAML, …) for terminal display, à la highlight.js.
// It uses chroma's lexers for language detection/tokenising, then paints each
// token with the *active theme's* palette (so highlighting follows the user's
// theme and degrades with the terminal's colour profile, like the rest of the
// UI). It is lossless: an unknown language or any lexer error returns the input
// unchanged, so callers can highlight unconditionally.
//
// Usage:
//
//	ui.Highlight(src, "handler.py")  // pick the lexer from a filename
//	ui.HighlightLang(doc, "json")    // when the language is already known
//
// The result is plain text with embedded ANSI styling; wrap/scroll it as usual.

// Highlight colours code, choosing a lexer from filename (falling back to a
// content analysis). Returns code unchanged when no lexer matches.
func Highlight(code, filename string) string {
	lexer := lexers.Match(filename)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	return highlightWith(lexer, code)
}

// HighlightLang colours code for a known language name or alias (e.g. "json",
// "python", "go"). Returns code unchanged when the language is unknown.
func HighlightLang(code, lang string) string {
	return highlightWith(lexers.Get(lang), code)
}

func highlightWith(lexer chroma.Lexer, code string) string {
	if lexer == nil {
		return code
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return code
	}
	var b strings.Builder
	b.Grow(len(code) + len(code)/4)
	for _, tok := range it.Tokens() {
		if color := tokenColor(tok.Type); color != "" {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(tok.Value))
		} else {
			b.WriteString(tok.Value)
		}
	}
	return b.String()
}

// tokenColor maps a chroma token type to a theme colour role, or "" to leave the
// terminal's default foreground (operators, punctuation, plain text) so the
// output stays calm rather than rainbow-coloured. Mapping to theme roles keeps
// highlighting consistent with the rest of the app and theme-switchable.
func tokenColor(t chroma.TokenType) string {
	// chroma's InCategory groups by top-level category (e.g. all Literals share
	// one), so strings and numbers need InSubCategory to be told apart.
	switch {
	case t.InCategory(chroma.Comment):
		return ColorMuted()
	case t.InSubCategory(chroma.LiteralString):
		return ColorSuccess()
	case t.InSubCategory(chroma.LiteralNumber):
		return ColorWarning()
	case t == chroma.KeywordType:
		return ColorInfo()
	case t.InCategory(chroma.Keyword):
		return ColorAccent()
	case t == chroma.NameFunction || t == chroma.NameClass || t == chroma.NameNamespace || t == chroma.NameDecorator:
		return ColorHeading()
	case t == chroma.NameBuiltin || t == chroma.NameBuiltinPseudo || t == chroma.NameAttribute:
		return ColorInfo()
	case t == chroma.NameTag:
		return ColorAccent()
	case t == chroma.Error:
		return ColorError()
	default:
		return ""
	}
}
