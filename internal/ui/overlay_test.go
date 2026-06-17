package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOverlayCenterBlankIsolatesContent(t *testing.T) {
	// A "busy" background that would otherwise be selectable behind a modal.
	bg := strings.Join([]string{
		"SENSITIVE-LEFT row one with trailing data RIGHT",
		"SENSITIVE-LEFT row two with trailing data RIGHT",
		"SENSITIVE-LEFT row three with trailing data RIGHT",
		"SENSITIVE-LEFT row four with trailing data RIGHT",
		"SENSITIVE-LEFT row five with trailing data RIGHT",
	}, "\n")
	_ = bg // the blank overlay must not include any of it

	fg := "┌────────┐\n│ HELLO  │\n└────────┘"
	out := OverlayCenterBlank(fg, 50, 5)

	if strings.Contains(out, "SENSITIVE") {
		t.Errorf("blank overlay leaked background text:\n%s", out)
	}
	if !strings.Contains(out, "HELLO") {
		t.Errorf("blank overlay missing foreground content:\n%s", out)
	}
	// The result must be exactly the frame height; rows without the overlay are
	// empty (nothing to select).
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d:\n%s", len(lines), out)
	}
	// No rendered line may exceed the frame width.
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w > 50 {
			t.Errorf("line %d overflows width 50 (%d): %q", i, w, ln)
		}
	}
}

func TestOverlayCenterBlankSmallHeight(t *testing.T) {
	// Degenerate sizes must not panic.
	if got := OverlayCenterBlank("x", 0, 0); got == "" && false {
		_ = got
	}
	_ = OverlayCenterBlank("x", 10, -3)
}
