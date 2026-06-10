// Context-aware keyboard shortcut hints.
//
// Every TUI in the application renders its bottom status bar through
// StatusBar, passing only the shortcuts that are actually usable in the
// current screen / focus / overlay. That keeps the hints honest: a key shown
// in the bar always does something right now.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// KeyHint is one keyboard shortcut shown in the status bar: the key (or key
// combination) and a short description of what it does in the current context.
type KeyHint struct {
	Key    string
	Action string
}

// H is a convenience constructor for a KeyHint.
func H(key, action string) KeyHint { return KeyHint{Key: key, Action: action} }

const hintSep = "  "

// renderHint renders one "key action" pair with the hint colors on the status
// bar background.
func renderHint(h KeyHint, keyStyle, actionStyle lipgloss.Style) string {
	if h.Action == "" {
		return keyStyle.Render(h.Key)
	}
	return keyStyle.Render(h.Key) + actionStyle.Render(" "+h.Action)
}

// RenderKeyHints renders the hints that fit within maxWidth, in order. Hints
// are dropped from the tail when space runs out, except that the final hint
// (by convention "? help" or a close/quit hint) is always kept and a "…"
// marker is inserted so the user knows more shortcuts exist.
func RenderKeyHints(hints []KeyHint, maxWidth int) string {
	if len(hints) == 0 || maxWidth <= 0 {
		return ""
	}
	bg := lipgloss.Color(ColorStatusBarBg())
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHintKey())).Background(bg).Bold(true)
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorHintText())).Background(bg)

	rendered := make([]string, len(hints))
	widths := make([]int, len(hints))
	total := 0
	for i, h := range hints {
		rendered[i] = renderHint(h, keyStyle, actionStyle)
		widths[i] = ansi.StringWidth(rendered[i])
		total += widths[i]
		if i > 0 {
			total += len(hintSep)
		}
	}
	if total <= maxWidth {
		return strings.Join(rendered, hintSep)
	}

	// Not everything fits: keep a prefix plus the last hint, with a "…"
	// marker in between.
	last := len(hints) - 1
	ellipsis := actionStyle.Render("…")
	reserved := ansi.StringWidth(ellipsis) + len(hintSep) + widths[last]

	var kept []string
	used := 0
	for i := 0; i < last; i++ {
		need := widths[i]
		if len(kept) > 0 {
			need += len(hintSep)
		}
		if used+need+len(hintSep)+reserved > maxWidth {
			break
		}
		kept = append(kept, rendered[i])
		used += need
	}
	kept = append(kept, ellipsis, rendered[last])
	out := strings.Join(kept, hintSep)
	if ansi.StringWidth(out) > maxWidth {
		// Pathologically narrow: show as much of the last hint as fits.
		out = ansi.Truncate(rendered[last], maxWidth, "…")
	}
	return out
}

// StatusBar renders the shared bottom status bar: contextual info text on the
// left and the context-aware shortcut hints right-aligned. Hints that do not
// fit are elided (see RenderKeyHints); the left text is truncated before any
// hint is sacrificed beyond that.
func StatusBar(width int, left string, hints []KeyHint) string {
	if width < 12 {
		width = 12
	}
	inner := width - 2 // StatusBarStyle pads one column each side

	// Give the hints at most ~2/3 of the bar, less if the left text is short.
	leftW := ansi.StringWidth(left)
	hintBudget := inner
	if left != "" {
		hintBudget = inner - leftW - len(hintSep)
	}
	hintStr := RenderKeyHints(hints, max(hintBudget, inner/3))
	hintW := ansi.StringWidth(hintStr)

	// Truncate the left text if it now collides with the hints.
	avail := inner - hintW - len(hintSep)
	if left != "" && leftW > avail {
		if avail > 1 {
			left = ansi.Truncate(left, avail, "…")
			leftW = ansi.StringWidth(left)
		} else {
			left = ""
			leftW = 0
		}
	}

	gap := inner - leftW - hintW
	if gap < 0 {
		gap = 0
	}
	content := left + strings.Repeat(" ", gap) + hintStr
	return StatusBarStyle(width).Render(content)
}
