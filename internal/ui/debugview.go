package ui

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/debuglog"
)

// DebugBody renders captured log entries into a themed, scrollable block for a
// debug activity pane: one line per entry as
//
//	HH:MM:SS.mmm  LEVEL  message  key=value …
//
// with the level coloured by severity. Shared by every TUI so the debug pane
// looks and behaves identically on each page. When there are no entries it
// returns a muted placeholder rather than an empty string.
func DebugBody(entries []debuglog.Entry, dropped int) string {
	if len(entries) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted())).
			Render("No activity recorded yet. Logs appear here as the tool scans.")
	}

	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))
	msgStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorText()))
	attrStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ColorMuted()))

	var b strings.Builder
	if dropped > 0 {
		b.WriteString(attrStyle.Render(fmt.Sprintf("…%d earlier line(s) scrolled off\n", dropped)))
	}
	for _, e := range entries {
		ts := timeStyle.Render(e.Time.Format("15:04:05.000"))
		lvl := debugLevelStyle(e.Level).Render(fmt.Sprintf("%-5s", levelLabel(e.Level)))
		line := ts + "  " + lvl + "  " + msgStyle.Render(e.Msg)
		if e.Attrs != "" {
			line += "  " + attrStyle.Render(e.Attrs)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func levelLabel(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "ERROR"
	case l >= slog.LevelWarn:
		return "WARN"
	case l >= slog.LevelInfo:
		return "INFO"
	default:
		return "DEBUG"
	}
}

func debugLevelStyle(l slog.Level) lipgloss.Style {
	var color string
	switch {
	case l >= slog.LevelError:
		color = ColorError()
	case l >= slog.LevelWarn:
		color = ColorWarning()
	case l >= slog.LevelInfo:
		color = ColorInfo()
	default:
		color = ColorMuted()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
}
