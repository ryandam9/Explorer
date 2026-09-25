package ui

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// WrapWords breaks one line at the last space that fits and mid-token when a
// token (an ARN, a JSON blob) has no space to break at. Spacing inside the
// line is preserved exactly — log output is often column-aligned or indented,
// and collapsing runs of spaces would quietly rewrite the message.
func WrapWords(line string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	var out []string
	for start := 0; start < len(runes); {
		if runewidth.StringWidth(string(runes[start:])) <= width {
			out = append(out, string(runes[start:]))
			break
		}
		// Walk forward while the line still fits, remembering the last point
		// we could break at without splitting a word.
		cut, lastSpace, w := start, -1, 0
		for i := start; i < len(runes); i++ {
			rw := runewidth.RuneWidth(runes[i])
			if w+rw > width {
				break
			}
			w += rw
			cut = i + 1
			if runes[i] == ' ' {
				lastSpace = i + 1
			}
		}
		if lastSpace > start {
			cut = lastSpace // prefer the word boundary
		}
		if cut == start {
			cut = start + 1 // a single rune wider than the column
		}
		out = append(out, strings.TrimRight(string(runes[start:cut]), " "))
		start = cut
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
