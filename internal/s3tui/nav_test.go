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
	// Up again.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.state != stateObjectList || m.prefix != "logs/" {
		t.Fatalf("after 2nd esc: state=%d prefix=%q, want logs/", m.state, m.prefix)
	}
	// Up to bucket root.
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

func TestDetailsDebounceOnlyFetchesSettledKey(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
		lastSelectedKey: "logs/a.txt"}

	// A debounce for a key the cursor has moved away from does nothing.
	m.Update(objectDetailsDebounceMsg{key: "logs/old.txt"})
	if m.detailsInFlight != "" {
		t.Errorf("stale debounce should not dispatch a fetch, got inFlight=%q", m.detailsInFlight)
	}

	// A debounce for the current key dispatches exactly one fetch.
	m.Update(objectDetailsDebounceMsg{key: "logs/a.txt"})
	if m.detailsInFlight != "logs/a.txt" {
		t.Fatalf("settled debounce should dispatch, inFlight=%q", m.detailsInFlight)
	}
	// A second debounce for the same in-flight key does not re-dispatch.
	before := m.detailsInFlight
	m.Update(objectDetailsDebounceMsg{key: "logs/a.txt"})
	if m.detailsInFlight != before {
		t.Errorf("duplicate debounce should be a no-op")
	}

	// When the result arrives, the in-flight marker clears.
	m.Update(objectDetailsMsg{key: "logs/a.txt", details: &ObjectDetails{}})
	if m.detailsInFlight != "" {
		t.Errorf("inFlight should clear on result, got %q", m.detailsInFlight)
	}
	if m.selectedDetails == nil {
		t.Errorf("details should be stored for the selected key")
	}
}
