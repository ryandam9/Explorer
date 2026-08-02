package cwtui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestHelpOverlayToggle(t *testing.T) {
	m := &model{groupSearch: textinput.New()}

	newModel, _ := m.Update(keyMsg("?"))
	m2 := newModel.(*model)
	if !m2.showHelp {
		t.Fatal("? should open the help overlay")
	}

	// Any key closes it — and only closes it (j must not move a selection).
	newModel, _ = m2.Update(keyMsg("j"))
	m3 := newModel.(*model)
	if m3.showHelp {
		t.Error("any key should close the help overlay")
	}
}

// While a search input has the keyboard, ? is text, not the help key.
func TestHelpKeyTypesIntoActiveSearch(t *testing.T) {
	m := &model{groupSearch: textinput.New(), focus: focusGroups, groupSearchActive: true}
	m.groupSearch.Focus()

	newModel, _ := m.Update(keyMsg("?"))
	m2 := newModel.(*model)
	if m2.showHelp {
		t.Error("? during search must not open help")
	}
	if got := m2.groupSearch.Value(); got != "?" {
		t.Errorf("? should be typed into the search input, got %q", got)
	}
}

// The help overlay must also open over the full log viewer, which owns all
// keys while active.
func TestHelpOverlayOverViewer(t *testing.T) {
	m := &model{groupSearch: textinput.New()}
	m.viewer.active = true
	m.viewer.search = textinput.New()
	m.viewer.grepInput = textinput.New()

	newModel, _ := m.Update(keyMsg("?"))
	m2 := newModel.(*model)
	if !m2.showHelp {
		t.Fatal("? over the viewer should open the help overlay")
	}
	if !m2.viewer.active {
		t.Error("opening help must not close the viewer")
	}

	newModel, _ = m2.Update(keyMsg("x"))
	m3 := newModel.(*model)
	if m3.showHelp {
		t.Error("any key should close the help overlay over the viewer")
	}
	if !m3.viewer.active {
		t.Error("closing help must leave the viewer open")
	}
}

// The overlay content must cover every pane's keys so nothing is only
// discoverable through the (eliding) status bar.
func TestHelpOverlayContent(t *testing.T) {
	m := &model{width: 100, height: 40}
	out := m.helpOverlay()
	for _, want := range []string{
		"Log groups", "Log streams", "Events", "Full log viewer", "Everywhere",
		"query window", "table view", "pan long messages", "Grep filter",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help overlay missing %q", want)
		}
	}
}
