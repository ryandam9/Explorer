package ui

import (
	"sort"
	"strings"
	"unicode"
)

// Ordering for keyboard-shortcut listings in the help overlays. Keys are sorted
// so a reader can find one quickly: symbol / navigation keys (↑/↓, <, /, ?, ~)
// group first, then word/letter keys alphabetically (case-insensitive, with a
// lowercase key just before its uppercase twin, e.g. "s" before "S").
//
// This is for static help panels only — the bottom status bar stays ordered by
// importance, since it elides the least-useful hints first on a narrow terminal.

// SortKeyLess reports whether shortcut key a should sort before key b.
func SortKeyLess(a, b string) bool {
	ra, oka := firstLetterRank(a)
	rb, okb := firstLetterRank(b)
	if oka != okb {
		// The symbol-only key (no letter) sorts first.
		return okb
	}
	if !oka { // both symbol-only: stable rune order
		return a < b
	}
	if ra != rb {
		return ra < rb
	}
	// Same first letter: order by the whole key, case-insensitively. When the
	// keys differ only in case (e.g. "s" vs "S"), put the lowercase one first —
	// lowercase has the higher code point, so reverse the comparison.
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a > b
}

// firstLetterRank returns the first Unicode letter in key, lowercased, and
// whether the key contains any letter at all.
func firstLetterRank(key string) (rune, bool) {
	for _, r := range key {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r), true
		}
	}
	return 0, false
}

// SortHelpSections sorts the shortcut rows within each section of a help body
// by key, leaving section headers and blank lines in place as anchors so the
// logical grouping (Navigation, Utility, …) is preserved. An "entry" line is
// one that begins with two spaces (the indentation every shortcut row uses);
// styled headers begin with an escape sequence or a non-space rune, and blank
// lines separate sections, so both act as block boundaries.
func SortHelpSections(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	for i := 0; i < len(out); {
		if !isHelpEntryLine(out[i]) {
			i++
			continue
		}
		j := i
		for j < len(out) && isHelpEntryLine(out[j]) {
			j++
		}
		block := out[i:j]
		sort.SliceStable(block, func(a, b int) bool {
			return SortKeyLess(helpEntryKey(block[a]), helpEntryKey(block[b]))
		})
		i = j
	}
	return out
}

func isHelpEntryLine(line string) bool {
	return strings.HasPrefix(line, "  ")
}

// helpEntryKey extracts the key column from a formatted help line: the text
// after the leading indent, up to the first run of two-or-more spaces that
// separates the key from its description.
func helpEntryKey(line string) string {
	s := strings.TrimLeft(line, " ")
	if idx := strings.Index(s, "  "); idx >= 0 {
		return strings.TrimSpace(s[:idx])
	}
	return strings.TrimSpace(s)
}
