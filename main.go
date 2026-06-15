package main

import (
	_ "embed"
	"log/slog"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/ryandam9/aws_explorer/cmd"
	"github.com/ryandam9/aws_explorer/internal/clilog"
)

// defaultConfig is the built-in configuration used when no config.yaml is
// found on disk, so the tool runs from any directory with zero setup.
//
//go:embed config.yaml
var defaultConfig []byte

func main() {
	// Structured logs go to stderr so they never interleave with results on
	// stdout. Humans at a terminal get plain text — tinted by level so a WARN or
	// ERROR stands out from the INFO stream (disabled by NO_COLOR or when piped);
	// pipes and log collectors get JSON. Interactive TUI commands and CLI scans
	// silence this logger (see cmd.SilenceScanLogs) and surface problems in their
	// own UI instead.
	var handler slog.Handler
	stderrIsTerm := isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	if stderrIsTerm {
		w := clilog.NewWriter(os.Stderr, clilog.ColorEnabled(stderrIsTerm))
		handler = slog.NewTextHandler(w, nil)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	}
	slog.SetDefault(slog.New(handler))

	cmd.SetDefaultConfig(defaultConfig)
	if err := cmd.Execute(); err != nil {
		// Cobra has already printed the error (and usage where relevant).
		os.Exit(1)
	}
}
