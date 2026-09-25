package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Painting starts every line on the canvas, re-applies it after every reset
// so unstyled gaps are covered, pads to the width and fills the height —
// without changing any visible text.
func TestPaintWith(t *testing.T) {
	const on = "\x1b[48;2;1;2;3m"
	frame := "a\x1b[1mb\x1b[0mc\nxy\x1b[49mz"
	out := paintWith(frame, on, 6, 3)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("height = %d, want 3", len(lines))
	}
	for i, l := range lines {
		if !strings.HasPrefix(l, on) {
			t.Errorf("line %d does not start on the canvas: %q", i, l)
		}
		if w := ansi.StringWidth(l); w != 6 {
			t.Errorf("line %d width %d, want 6", i, w)
		}
	}
	if !strings.Contains(lines[0], "\x1b[0m"+on+"c") {
		t.Errorf("canvas not re-applied after a reset: %q", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b[49m"+on+"z") {
		t.Errorf("canvas not re-applied after default-background: %q", lines[1])
	}
	if got := ansi.Strip(out); got != "abc   \nxyz   \n      " {
		t.Errorf("visible text changed: %q", got)
	}
}

// Off (the default), Paint returns the frame untouched.
func TestPaintOffIsIdentity(t *testing.T) {
	SetPaintBackground(false)
	if got := Paint("a\x1b[0mb", 10, 5); got != "a\x1b[0mb" {
		t.Errorf("Paint with painting off changed the frame: %q", got)
	}
}
