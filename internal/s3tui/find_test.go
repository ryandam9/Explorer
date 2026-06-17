package s3tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// objectModelForFind builds a Model on the object list with the given names as
// its rows, ready for jumpToMatch.
func objectModelForFind(names []string) *Model {
	m := &Model{state: stateObjectList, focus: focusObjects}
	m.initObjectTable()
	m.findInput = textinput.New()
	m.objectMaps = make([]map[string]string, len(names))
	rows := make([]table.Row, len(names))
	for i, n := range names {
		m.objectMaps[i] = map[string]string{"name": n, "type": "FILE"}
		rows[i] = table.Row{n}
	}
	m.objectTable.SetRows(rows)
	return m
}

func TestJumpToMatchObjects(t *testing.T) {
	names := []string{"apple.csv", "banana.csv", "cherry.csv", "banana-split.csv"}
	m := objectModelForFind(names)

	// Substring, case-insensitive: first "ban" match is index 1.
	m.jumpToMatch("BAN", 0, 1)
	if got := m.objectTable.Cursor(); got != 1 {
		t.Errorf("jump 'BAN' cursor = %d, want 1", got)
	}
	if m.findMatches != 2 {
		t.Errorf("findMatches = %d, want 2", m.findMatches)
	}

	// Next match forward from index 2 wraps to index 3 (banana-split).
	m.jumpToMatch("ban", 2, 1)
	if got := m.objectTable.Cursor(); got != 3 {
		t.Errorf("next 'ban' cursor = %d, want 3", got)
	}

	// Substring anywhere in the name.
	m.jumpToMatch("rry", 0, 1)
	if got := m.objectTable.Cursor(); got != 2 {
		t.Errorf("jump 'rry' cursor = %d, want 2", got)
	}
}

func TestJumpToMatchNoMatchKeepsCursor(t *testing.T) {
	m := objectModelForFind([]string{"a", "b", "c"})
	m.objectTable.SetCursor(1)
	m.jumpToMatch("zzz", 0, 1)
	if got := m.objectTable.Cursor(); got != 1 {
		t.Errorf("no-match cursor moved to %d, want 1 (unchanged)", got)
	}
	if m.findMatches != 0 {
		t.Errorf("findMatches = %d, want 0", m.findMatches)
	}
}

func TestJumpToMatchBlankQuery(t *testing.T) {
	m := objectModelForFind([]string{"a", "b"})
	m.objectTable.SetCursor(1)
	m.jumpToMatch("   ", 0, 1)
	if got := m.objectTable.Cursor(); got != 1 {
		t.Errorf("blank query moved cursor to %d, want 1", got)
	}
}

func TestJumpToMatchBackward(t *testing.T) {
	names := []string{"log-1", "log-2", "data", "log-3"}
	m := objectModelForFind(names)
	m.objectTable.SetCursor(2)
	// Search backward from index 1 → index 1 (log-2) is itself a match.
	m.jumpToMatch("log", 1, -1)
	if got := m.objectTable.Cursor(); got != 1 {
		t.Errorf("backward 'log' from 1 cursor = %d, want 1", got)
	}
}

func TestStartCloseFind(t *testing.T) {
	m := objectModelForFind([]string{"a"})
	m.startFind()
	if !m.finding || m.focus != focusFind {
		t.Fatalf("startFind: finding=%v focus=%d", m.finding, m.focus)
	}
	m.closeFind()
	if m.finding || m.focus != focusObjects {
		t.Errorf("closeFind: finding=%v focus=%d, want false/focusObjects", m.finding, m.focus)
	}
}

// TestColonOpensFindAndJumps drives the feature end-to-end through Update:
// ":" opens the prompt, typing moves the highlight live, Enter keeps the cursor.
func TestColonOpensFindAndJumps(t *testing.T) {
	m := objectModelForFind([]string{"alpha", "beta", "gamma", "delta"})
	m.width, m.height = 100, 30

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if !m.finding || m.focus != focusFind {
		t.Fatalf("':' did not open find: finding=%v focus=%d", m.finding, m.focus)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if got := m.objectTable.Cursor(); got != 3 {
		t.Errorf("after typing 'de' cursor = %d, want 3 (delta)", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.finding {
		t.Errorf("Enter did not close the find prompt")
	}
	if got := m.objectTable.Cursor(); got != 3 {
		t.Errorf("cursor after Enter = %d, want 3 (kept)", got)
	}
}

// TestColonEscRestoresAndDoesNotFilter confirms Esc closes find without ever
// removing rows (no filtering).
func TestColonEscRestoresFocus(t *testing.T) {
	m := objectModelForFind([]string{"alpha", "beta"})
	m.width, m.height = 100, 30
	rowsBefore := len(m.objectTable.Rows())

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.finding || m.focus != focusObjects {
		t.Errorf("Esc left finding=%v focus=%d", m.finding, m.focus)
	}
	if got := len(m.objectTable.Rows()); got != rowsBefore {
		t.Errorf("rows changed to %d (want %d) — jump must not filter", got, rowsBefore)
	}
}

func TestFindNamesBuckets(t *testing.T) {
	m := &Model{state: stateBucketList, focus: focusBuckets}
	m.initBucketTable()
	// Bucket rows: col 0 is the sequence number, col 1 the name.
	m.bucketTable.SetRows([]table.Row{
		{"1", "prod-logs", "us-east-1"},
		{"2", "dev-data", "eu-west-1"},
	})
	names := m.findNames()
	if len(names) != 2 || names[0] != "prod-logs" || names[1] != "dev-data" {
		t.Errorf("findNames = %v, want [prod-logs dev-data]", names)
	}
}
