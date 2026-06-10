package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func hintFixture() []KeyHint {
	return []KeyHint{
		H("↑/↓", "navigate"),
		H("Enter", "open"),
		H("/", "filter"),
		H("r", "refresh"),
		H("?", "help"),
	}
}

func TestRenderKeyHintsFitsAll(t *testing.T) {
	out := RenderKeyHints(hintFixture(), 200)
	plain := ansi.Strip(out)
	for _, want := range []string{"↑/↓ navigate", "Enter open", "/ filter", "r refresh", "? help"} {
		if !strings.Contains(plain, want) {
			t.Errorf("expected %q in %q", want, plain)
		}
	}
}

func TestRenderKeyHintsElidesButKeepsLast(t *testing.T) {
	hints := hintFixture()
	out := RenderKeyHints(hints, 30)
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "? help") {
		t.Errorf("last hint must survive elision, got %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Errorf("elision marker missing in %q", plain)
	}
	if w := ansi.StringWidth(out); w > 30 {
		t.Errorf("rendered width %d exceeds budget 30", w)
	}
}

func TestRenderKeyHintsEmpty(t *testing.T) {
	if out := RenderKeyHints(nil, 50); out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
	if out := RenderKeyHints(hintFixture(), 0); out != "" {
		t.Errorf("expected empty output for zero width, got %q", out)
	}
}

func TestStatusBarContainsLeftAndHints(t *testing.T) {
	out := StatusBar(80, "Buckets: 12", []KeyHint{H("Enter", "open"), H("?", "help")})
	plain := ansi.Strip(out)
	if !strings.Contains(plain, "Buckets: 12") {
		t.Errorf("left text missing from %q", plain)
	}
	if !strings.Contains(plain, "Enter open") || !strings.Contains(plain, "? help") {
		t.Errorf("hints missing from %q", plain)
	}
}

func TestStatusBarTruncatesLongLeft(t *testing.T) {
	long := strings.Repeat("x", 200)
	out := StatusBar(60, long, []KeyHint{H("?", "help")})
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 62 { // width + MaxWidth slack
			t.Errorf("status bar line width %d exceeds 62", w)
		}
	}
	if !strings.Contains(ansi.Strip(out), "? help") {
		t.Errorf("hint lost when left text is long: %q", ansi.Strip(out))
	}
}
