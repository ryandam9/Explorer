package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestProgressBar(t *testing.T) {
	for _, f := range []float64{-1, 0, 0.37, 1, 2} {
		if w := ansi.StringWidth(ProgressBar(f, 20)); w != 20 {
			t.Errorf("ProgressBar(%v) width %d, want 20", f, w)
		}
	}
	if Fraction(3, 0) != 0 || Fraction(1, 4) != 0.25 {
		t.Error("Fraction")
	}
}
