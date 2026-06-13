package cmd

import (
	"context"
	"log/slog"
	"os"

	"github.com/ryandam9/aws_explorer/internal/debuglog"
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
// Logs are never written to the terminal here. They are always captured into
// the in-memory debuglog sink so the TUIs can show a live debug pane, and,
// when AWS_EXPLORER_LOG=/path/to/file is set, additionally written to that
// file as JSON for offline debugging.
func SilenceScanLogs() {
	handlers := []slog.Handler{debuglog.NewHandler(debuglog.Default)}
	if path := os.Getenv("AWS_EXPLORER_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			handlers = append(handlers, slog.NewJSONHandler(f, nil))
		}
	}
	slog.SetDefault(slog.New(fanout(handlers)))
}

// fanoutHandler forwards every record to all of the given handlers, so scan
// activity can be captured for the debug pane and optionally persisted to a
// file at the same time.
type fanoutHandler []slog.Handler

func fanout(handlers []slog.Handler) slog.Handler { return fanoutHandler(handlers) }

func (f fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range f {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (f fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(fanoutHandler, len(f))
	for i, h := range f {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (f fanoutHandler) WithGroup(name string) slog.Handler {
	out := make(fanoutHandler, len(f))
	for i, h := range f {
		out[i] = h.WithGroup(name)
	}
	return out
}
