package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestVScrollbarBlankWhenContentFits(t *testing.T) {
	got := ansi.Strip(VScrollbar(5, 10, 10, 0))
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), got)
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			t.Errorf("line %d should be blank when content fits, got %q", i, ln)
		}
	}
}

func TestVScrollbarThumbMovesWithOffset(t *testing.T) {
	const height, total, visible = 6, 60, 10

	thumbRows := func(offset int) (first, count int) {
		lines := strings.Split(ansi.Strip(VScrollbar(height, total, visible, offset)), "\n")
		first = -1
		for i, ln := range lines {
			if strings.Contains(ln, "┃") {
				if first < 0 {
					first = i
				}
				count++
			}
		}
		return first, count
	}

	topFirst, topCount := thumbRows(0)
	if topFirst != 0 {
		t.Errorf("at offset 0 the thumb should sit at the top, got first row %d", topFirst)
	}
	if topCount < 1 {
		t.Fatal("thumb should always be at least one cell")
	}

	botFirst, _ := thumbRows(total - visible)
	if botFirst <= topFirst {
		t.Errorf("scrolling down should move the thumb lower: top=%d bottom=%d", topFirst, botFirst)
	}
}
