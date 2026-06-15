package cwtui

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

func TestLogLineColor(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{"error keyword", "2026-06-12 ERROR connection refused", ui.ColorError()},
		{"level=error", "ts=1 level=error msg=boom", ui.ColorError()},
		{"failed", "request failed after 3 retries", ui.ColorError()},
		{"panic", "panic: runtime error: nil map", ui.ColorError()},
		{"warn", "[WARN] disk almost full", ui.ColorWarning()},
		{"deprecated", "this API is deprecated", ui.ColorWarning()},
		{"info", "INFO started server on :8080", ui.ColorInfo()},
		{"notice", "notice: cache warmed", ui.ColorInfo()},
		{"debug", "DEBUG handler entered", ui.ColorMuted()},
		{"trace", "trace span opened", ui.ColorMuted()},
		{"plain", "just a normal line of output", ui.ColorText()},
		{"info-substring-not-matched", "reinforcement learning step", ui.ColorText()},
		{"error-beats-info", "INFO retry failed", ui.ColorError()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := logLineColor(tt.line); got != tt.want {
				t.Errorf("logLineColor(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}
