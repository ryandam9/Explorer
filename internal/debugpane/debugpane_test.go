package debugpane

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOpenAndClose(t *testing.T) {
	var m Model
	if m.Visible() {
		t.Fatal("zero value should be closed")
	}
	m.Open(120, 40)
	if !m.Visible() {
		t.Fatal("Open should make the pane visible")
	}
	m.Close()
	if m.Visible() {
		t.Fatal("Close should hide the pane")
	}
}

func TestHandleInputConsumesKeysAndClosesOnEscape(t *testing.T) {
	var m Model
	m.Open(120, 40)

	// A scroll key is consumed but leaves the pane open.
	if !m.HandleInput(tea.KeyMsg{Type: tea.KeyDown}) {
		t.Fatal("key input should be consumed while visible")
	}
	if !m.Visible() {
		t.Fatal("scroll key should not close the pane")
	}

	// Esc closes it.
	if !m.HandleInput(tea.KeyMsg{Type: tea.KeyEscape}) {
		t.Fatal("esc should be consumed")
	}
	if m.Visible() {
		t.Fatal("esc should close the pane")
	}
}

func TestHandleInputLetsNonInputFallThrough(t *testing.T) {
	var m Model
	m.Open(120, 40)
	// A non key/mouse message must NOT be consumed, so the caller keeps
	// processing it (e.g. scan chunks) while the pane is open.
	if m.HandleInput(struct{ scanChunk int }{}) {
		t.Fatal("non-input message must fall through (return false)")
	}
	if !m.Visible() {
		t.Fatal("non-input message should not affect visibility")
	}
}

func TestOverlayPassesThroughWhenClosed(t *testing.T) {
	var m Model
	base := "the live screen"
	if got := m.Overlay(base, 80, 24); got != base {
		t.Fatalf("closed pane should return base unchanged, got %q", got)
	}
}

func TestRefreshNoopWhenClosed(t *testing.T) {
	var m Model
	m.Refresh() // must not panic on the zero value
}
