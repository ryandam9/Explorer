package main

import (
	"log/slog"
	"os"

	"github.com/user/aws_explorer/cmd"
)

func main() {
	// Initialize default structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := cmd.Execute(); err != nil {
		slog.Error("Application failed", "error", err)
		os.Exit(1)
	}
}
