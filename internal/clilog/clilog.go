// Package clilog colorizes the structured logs the CLI prints to a terminal.
//
// Non-TUI commands (expiring, find, whereused, …) log to stderr through slog's
// text handler, which emits uncolored lines like:
//
//	time=2026-06-15T13:20:44+10:00 level=INFO msg="Initializing AWS config"
//	time=2026-06-15T13:20:45+10:00 level=WARN msg="Not authorized to call …"
//
// On a busy run those scroll past in a wall of identical-looking text, so a
// WARN or ERROR is easy to miss. This package wraps the handler's output
// writer and tints each line by its level= field — WARN/ERROR lines stand out
// in full colour, INFO/DEBUG get just their level token coloured so the bulk
// of the line stays readable. It honours the NO_COLOR convention and only
// engages on a terminal, so piped/redirected logs stay plain.
package clilog

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI SGR codes. Kept as raw escapes (rather than lipgloss) so colouring is
// gated purely on the stderr terminal check in NewWriter, independent of
// lipgloss's stdout-based colour-profile detection.
const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	green  = "\x1b[32m"
	gray   = "\x1b[90m"
	cyan   = "\x1b[36m"
)

// ColorEnabled reports whether colored log output should be used, given whether
// stderr is a terminal. It honors the NO_COLOR convention (https://no-color.org).
func ColorEnabled(stderrIsTerminal bool) bool {
	return stderrIsTerminal && os.Getenv("NO_COLOR") == ""
}

// NewWriter wraps w so that each slog text-handler line written through it is
// colored by its level= field. When color is false it returns w unchanged, so
// callers can wire it in unconditionally.
func NewWriter(w io.Writer, color bool) io.Writer {
	if !color {
		return w
	}
	return colorWriter{w: w}
}

// colorWriter colorizes whole log records. slog's text handler builds each
// record into one buffer and writes it in a single Write call, so a line-based
// transform here is reliable.
type colorWriter struct{ w io.Writer }

func (c colorWriter) Write(p []byte) (int, error) {
	if _, err := io.WriteString(c.w, colorize(string(p))); err != nil {
		return 0, err
	}
	// Report the original length: slog only cares that all of p was consumed,
	// not how many bytes the (longer, escape-laden) output actually was.
	return len(p), nil
}

// colorize tints every level-bearing line in s. Lines without a level= field
// (a bare status print, a blank line) pass through untouched.
func colorize(s string) string {
	if !strings.Contains(s, "level=") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = colorizeLine(ln)
	}
	return strings.Join(lines, "\n")
}

func colorizeLine(line string) string {
	idx := strings.Index(line, "level=")
	if idx < 0 {
		return line
	}
	rest := line[idx+len("level="):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = len(rest)
	}
	level := rest[:end]

	switch {
	case strings.HasPrefix(level, "ERROR"):
		// Errors are what the user is scanning for — colour the whole line.
		return bold + red + line + reset
	case strings.HasPrefix(level, "WARN"):
		return yellow + line + reset
	case strings.HasPrefix(level, "INFO"):
		return colorToken(line, idx, "level="+level, green)
	case strings.HasPrefix(level, "DEBUG"):
		return colorToken(line, idx, "level="+level, gray)
	default:
		return line
	}
}

// colorToken wraps just the level= token at idx in color, leaving the rest of
// the line in the terminal's default foreground so it stays easy to read.
func colorToken(line string, idx int, token, color string) string {
	return line[:idx] + bold + color + token + reset + line[idx+len(token):]
}

// levelWidth pads every level label to a common width so a column of mixed
// INFO / WARNING / ERROR tags stays aligned. "WARNING" is the longest label
// we emit, so the column is sized to it.
const levelWidth = 7

// levelColor maps a severity label to its ANSI colour, matching the scheme
// colorize() uses for slog records (ERROR red, WARN yellow, INFO green, DEBUG
// gray). An unrecognised label gets no colour.
func levelColor(label string) string {
	switch {
	case strings.HasPrefix(label, "ERROR"):
		return red
	case strings.HasPrefix(label, "WARN"):
		return yellow
	case strings.HasPrefix(label, "INFO"):
		return green
	case strings.HasPrefix(label, "DEBUG"):
		return gray
	default:
		return ""
	}
}

// LevelTag returns a fixed-width, coloured severity label (e.g. "INFO   ",
// "WARNING", "ERROR  ") for ad-hoc CLI status lines that are not slog records
// but should sit visually alongside the structured logs this package colours.
// When color is false the tag is padded but left plain. Unknown levels are
// padded and uncoloured.
func LevelTag(level string, color bool) string {
	label := strings.ToUpper(strings.TrimSpace(level))
	padded := fmt.Sprintf("%-*s", levelWidth, label)
	c := levelColor(label)
	if !color || c == "" {
		return padded
	}
	return bold + c + padded + reset
}

// Highlight emphasises a user-supplied value (an ARN, a resource id) so it
// stands out when echoed back in a status line. It renders bold cyan on a
// terminal and returns the string unchanged when color is false or empty.
func Highlight(s string, color bool) string {
	if !color || s == "" {
		return s
	}
	return bold + cyan + s + reset
}

// Statusf prints a leveled status line to w: a coloured level tag followed by
// the formatted message. It mirrors the look of the colorized slog records so
// one-off CLI prints don't look out of place next to them. Pass color from
// ColorEnabled.
func Statusf(w io.Writer, color bool, level, format string, args ...any) {
	fmt.Fprintf(w, "%s %s\n", LevelTag(level, color), fmt.Sprintf(format, args...))
}
