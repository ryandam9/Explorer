package s3tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Pressing / to edit the prefix should land the cursor at the end of the
// current path, ready to extend it, not at column 0.
func TestSlashPutsCursorAtEndOfPrefix(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects, prefix: "logs/2026/"}
	m.initObjectTable()
	m.prefixInput = textinput.New()

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	if m.focus != focusPrefixInput {
		t.Fatalf("/ should focus the prefix input, focus=%d", m.focus)
	}
	if got := m.prefixInput.Value(); got != "logs/2026/" {
		t.Fatalf("prefix input value = %q, want the current path", got)
	}
	if pos, end := m.prefixInput.Position(), len([]rune("logs/2026/")); pos != end {
		t.Errorf("cursor position = %d, want end (%d)", pos, end)
	}
}

// The bucket search likewise opens with the cursor at the end of any existing
// query.
func TestSlashBucketSearchCursorAtEnd(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateBucketList, focus: focusBuckets}
	m.initBucketTable()
	m.bucketSearch = textinput.New()
	m.bucketSearch.SetValue("prod")

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})

	if !m.inBucketSearch || m.focus != focusBucketSearch {
		t.Fatalf("/ should open bucket search, inSearch=%v focus=%d", m.inBucketSearch, m.focus)
	}
	if pos, end := m.bucketSearch.Position(), len([]rune("prod")); pos != end {
		t.Errorf("cursor position = %d, want end (%d)", pos, end)
	}
}
