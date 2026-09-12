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
	// Wrapped action text may break a phrase across lines, so matching runs
	// against a layout-normalized rendering: box-drawing characters removed
	// and all whitespace collapsed.
	normalize := func(s string) string {
		s = strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(s)
		return strings.Join(strings.Fields(s), " ")
	}

	// Both layouts — two columns on a wide terminal, one column on a narrow
	// one — must carry the full key reference.
	for _, width := range []int{160, 60} {
		m := &model{width: width, height: 40}
		out := normalize(m.helpOverlay())
		for _, want := range []string{
			// Section titles carry the panes' fixed names.
			"[1] Log groups", "[2] Log streams", "[3] Log events", "Event record",
			"Log viewer", "Everywhere",
			"query window", "table view", "Grep filter",
			// Every binding must be discoverable here, not only via the
			// (eliding) status bar.
			"Download", "Shift+Tab", "Backspace", "g/Home, G/End", "n / N",
			"Ctrl+U", "Copy the full record", "Clear all filters",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("help overlay (width %d) missing %q", width, want)
			}
		}
	}
}
