package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestConfirmButtons(t *testing.T) {
	got := ansi.Strip(ConfirmButtons("y  Download", "Esc  Cancel", false))
	if got != "  y  Download       Esc  Cancel  " {
		t.Errorf("buttons = %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Error("buttons must stay on one line")
	}
}
