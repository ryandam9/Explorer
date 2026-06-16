package s3tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEscGoesUpOneFolderThenToBuckets(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
		prefix: "logs/2026/06/", bucket: "b"}
	m.initObjectTable()

	// First esc: up to the parent prefix, still in the object list.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateObjectList || m.prefix != "logs/2026/" {
		t.Fatalf("after 1st esc: state=%d prefix=%q, want object list at logs/2026/", m.state, m.prefix)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateObjectList || m.prefix != "logs/" {
		t.Fatalf("after 2nd esc: state=%d prefix=%q, want logs/", m.state, m.prefix)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateObjectList || m.prefix != "" {
		t.Fatalf("after 3rd esc: state=%d prefix=%q, want object root", m.state, m.prefix)
	}
	// At the root, esc finally returns to the bucket list.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateBucketList || m.bucket != "" {
		t.Fatalf("after 4th esc: state=%d bucket=%q, want bucket list", m.state, m.bucket)
	}
}

func newObjectListModel() *Model {
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
		prefix: "logs/", objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.objectMaps = []map[string]string{
		{"name": "a.txt", "type": "FILE", "size": "1 B"},
		{"name": "b.txt", "type": "FILE", "size": "2 B"},
	}
	m.objectTable.SetRows(m.buildObjectRows())
	m.lastSelectedKey = "logs/a.txt"
	return m
}

// Scrolling must not fetch metadata — that was the source of the lag.
func TestScrollingDoesNotFetchDetails(t *testing.T) {
	m := newObjectListModel()
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.detailsLoading {
		t.Error("moving the cursor must not start a details fetch")
	}
	if m.selectedDetails != nil {
		t.Error("no details should be present until requested")
	}
}

// Pressing d fetches on demand; the result is cached and shown.
func TestDetailsFetchedOnDemand(t *testing.T) {
	m := newObjectListModel()

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m.detailsLoading {
		t.Fatal("pressing d should start a details fetch")
	}

	det := &ObjectDetails{ContentType: "text/plain"}
	m.Update(objectDetailsMsg{key: "logs/a.txt", details: det})
	if m.detailsLoading {
		t.Error("loading should clear on result")
	}
	if m.selectedDetails != det {
		t.Error("fetched details should be shown for the selected object")
	}
	if m.objectDetailsCache["logs/a.txt"] != det {
		t.Error("details should be cached for instant revisits")
	}

	// Revisiting a cached key shows details with no new fetch.
	m.selectedDetails = nil
	m.objectTable.SetCursor(0)
	m.Update(tea.KeyMsg{Type: tea.KeyDown}) // to b.txt
	m.Update(tea.KeyMsg{Type: tea.KeyUp})   // back to a.txt
	if m.selectedDetails != det || m.detailsLoading {
		t.Errorf("revisit should serve cache without fetching: details=%v loading=%v", m.selectedDetails, m.detailsLoading)
	}
}
