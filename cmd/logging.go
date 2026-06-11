package cmd

import (
	"io"
	"log/slog"
	"os"
)

// SilenceScanLogs redirects the global slog logger away from the terminal
// for commands that own their output: every TUI and the streaming CLI scan.
//
// The TUIs render with Bubble Tea's alternate screen buffer, so any stray
// log line — for example the per-region "Access denied, skipping region"
// warnings emitted while scanning — is painted directly over the interface
// and corrupts the display. The CLI scan streams a table to stdout and
// summarizes collection errors after the run, so the raw log stream is
// redundant noise there too.
//
// Logs are discarded by default. Set AWS_EXPLORER_LOG=/path/to/file to
// capture them for debugging without disturbing the screen.
func SilenceScanLogs() {
	var w io.Writer = io.Discard
	if path := os.Getenv("AWS_EXPLORER_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			w = f
		}
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(w, nil)))
}
