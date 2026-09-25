package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Every line of a titled panel is exactly the requested width, the title sits
// in the top border, long text wraps inside, and short bodies pad to minHeight.
func TestTitledPanel(t *testing.T) {
	body := "short\n" + strings.Repeat("word ", 12)
	out := TitledPanel("Health", body, 30, 6, false)
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 30 {
			t.Errorf("line %d width %d, want 30: %q", i, w, l)
		}
	}
	if !strings.HasPrefix(ansi.Strip(lines[0]), "╭─┤ Health ├─") || !strings.HasSuffix(ansi.Strip(lines[0]), "╮") {
		t.Errorf("top border = %q", lines[0])
	}
	if len(lines) != 6+2 {
		t.Errorf("height = %d, want minHeight 6 + 2 borders", len(lines))
	}
	// A title longer than the box is cut, never overflowing the border.
	long := TitledPanel(strings.Repeat("T", 50), "x", 20, 0, true)
	if w := ansi.StringWidth(strings.Split(long, "\n")[0]); w != 20 {
		t.Errorf("long title width %d, want 20", w)
	}
}

// A wrapped line keeps its own indent on the continuation lines.
func TestTitledPanelHangingIndent(t *testing.T) {
	out := TitledPanel("P", "    \"Resource\": \""+strings.Repeat("x", 40)+"\"", 30, 0, false)
	lines := strings.Split(ansi.Strip(out), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected the long line to wrap:\n%s", out)
	}
	for _, l := range lines[2 : len(lines)-1] {
		if !strings.HasPrefix(l, "│     ") {
			t.Errorf("continuation should keep the 4-space indent: %q", l)
		}
	}
}

// The bottom border carries the info items right-aligned, dropping the
// leftmost ones when they don't fit, and a Divider line becomes a section rule.
func TestBoxInfoAndDividers(t *testing.T) {
	out := Box{Title: "T", Info: []string{"sort: name", "3/120"}, Width: 40}.Render("a\n" + Divider("Net") + "\nb")
	lines := strings.Split(ansi.Strip(out), "\n")
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %d width %d, want 40: %q", i, w, l)
		}
	}
	if !strings.HasSuffix(lines[len(lines)-1], "┤ sort: name ├─┤ 3/120 ├─╯") {
		t.Errorf("bottom = %q", lines[len(lines)-1])
	}
	if !strings.HasPrefix(lines[2], "├─ Net ─") || !strings.HasSuffix(lines[2], "┤") {
		t.Errorf("divider = %q", lines[2])
	}

	narrow := Box{Info: []string{"a very long first item", "9/9"}, Width: 16}.Render("x")
	bottom := ansi.Strip(strings.Split(narrow, "\n")[2])
	if strings.Contains(bottom, "long") || !strings.Contains(bottom, "┤ 9/9 ├") || ansi.StringWidth(bottom) != 16 {
		t.Errorf("items that don't fit are dropped from the left: %q", bottom)
	}
}

func TestSectionHeading(t *testing.T) {
	got := ansi.Strip(SectionHeading("Services", 20))
	if got != "Services ───────────" || ansi.StringWidth(got) != 20 {
		t.Errorf("SectionHeading = %q", got)
	}
	if w := ansi.StringWidth(ansi.Strip(SectionHeading("A very long heading indeed", 10))); w > 10 {
		t.Errorf("long heading overflows: width %d", w)
	}
}
