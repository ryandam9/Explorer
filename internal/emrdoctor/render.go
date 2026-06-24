package emrdoctor

import (
	"fmt"
	"io"
	"strings"
)

// ANSI SGR codes, kept raw so colouring is gated purely on the caller's terminal
// check (matching internal/clilog), independent of any colour-profile probe.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiGray   = "\x1b[90m"
)

const hintWrap = 74 // wrap hint text to this width before the indent

// Render writes the report as step-by-step lines plus a one-line summary. color
// enables ANSI markers; off a terminal it degrades to plain ASCII tags.
func Render(w io.Writer, r *Report, color bool) {
	for _, c := range r.Checks {
		fmt.Fprintf(w, "  %s %s\n", marker(c.Status, color), line(c))
		if c.Hint != "" && (c.Status == StatusFail || c.Status == StatusWarn) {
			for _, hl := range wrapText(c.Hint, hintWrap) {
				fmt.Fprintf(w, "      %s %s\n", tint("→", ansiGray, color), tint(hl, ansiGray, color))
			}
		}
	}
	ok, fail, warn, skip := r.Counts()
	fmt.Fprintf(w, "\n%s\n", summaryLine(ok, fail, warn, skip, color))
}

// marker returns the coloured status glyph + fixed-width tag, so a column of
// mixed statuses stays aligned.
func marker(s Status, color bool) string {
	var glyph, tag, c string
	switch s {
	case StatusOK:
		glyph, tag, c = "✓", "OK  ", ansiGreen
	case StatusFail:
		glyph, tag, c = "✗", "FAIL", ansiRed
	case StatusWarn:
		glyph, tag, c = "!", "WARN", ansiYellow
	case StatusSkip:
		glyph, tag, c = "·", "SKIP", ansiGray
	default:
		glyph, tag, c = "?", "????", ""
	}
	out := glyph + " " + tag
	if !color || c == "" {
		return out
	}
	return ansiBold + c + out + ansiReset
}

func line(c Check) string {
	if c.Detail == "" {
		return c.Name
	}
	return fmt.Sprintf("%-26s %s", c.Name, c.Detail)
}

// summaryLine renders the closing tally and verdict.
func summaryLine(ok, fail, warn, skip int, color bool) string {
	parts := []string{fmt.Sprintf("%d OK", ok)}
	if fail > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", fail))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d warning%s", warn, plural(warn)))
	}
	if skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skip))
	}
	body := "Summary: " + strings.Join(parts, " · ")
	switch {
	case fail > 0:
		body += " — fix the first ✗ above and re-run."
		return tint(body, ansiRed, color)
	case warn > 0:
		body += " — reachable, with warnings above."
		return tint(body, ansiYellow, color)
	default:
		body += " — all good."
		return tint(body, ansiGreen, color)
	}
}

func tint(s, c string, color bool) string {
	if !color || c == "" {
		return s
	}
	return c + s + ansiReset
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// wrapText soft-wraps s on word boundaries to at most width columns per line. A
// single token longer than width is left intact on its own line rather than
// broken.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := words[0]
	for _, w := range words[1:] {
		if len(cur)+1+len(w) > width {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur += " " + w
	}
	return append(lines, cur)
}
