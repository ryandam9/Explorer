package main

import (
	"log/slog"
	"os"

	"github.com/user/aws_explorer/cmd"
)

func main() {
	// Initialize default structured logger. Logs go to stderr so they never
	// interleave with table/JSON/CSV results written to stdout. Interactive TUI
	// commands silence this logger entirely (see cmd.SilenceLogsForTUI) because
	// they take over the terminal with an alternate screen buffer.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := cmd.Execute(); err != nil {
		slog.Error("Application failed", "error", err)
		os.Exit(1)
	}
}
