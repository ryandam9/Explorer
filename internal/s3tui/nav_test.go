package s3tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/table"
)

func TestBucketSummaryOnDemand(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateBucketList, focus: focusBuckets,
		bucketRegionCache:  map[string]string{},
		bucketDetailsCache: map[string]*BucketDetails{}}
	m.initBucketTable()
	m.bucketTable.SetRows([]table.Row{
		{"1", "my-bucket", "us-east-1", "2026-01-01"},
		{"2", "other-bucket", "us-east-1", "2026-01-02"},
	})

	// Scrolling the bucket list selects a bucket but fetches no summary.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.detailsLoading {
		t.Error("scrolling buckets must not fetch the (~19-call) summary")
	}
	if m.selectedBucketDetails != nil {
		t.Error("no bucket summary should load until requested")
	}

	// Pressing d opens the full detail view (where the fetch happens on demand).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if m.state != stateBucketDetail {
		t.Errorf("d should open the bucket detail view, state=%d", m.state)
	}
}

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

// Sorting reorders the list under a stationary cursor. Pressing "d" must still
// fetch the row now under the cursor — and the result must be shown — rather
// than being blocked or discarded because internal state lagged the sort.
func TestDetailsFetchAfterSort(t *testing.T) {
	m := newObjectListModel()
	m.sortAsc = true // pressing R flips to descending so b.txt sorts to row 0
	// Cursor on row 0. Reverse the sort so b.txt is now the row under the cursor.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	if got, _ := m.selectedObjectKey(); got != "logs/b.txt" {
		t.Fatalf("after reverse-sort, row 0 should be b.txt, got %q", got)
	}

	// Pressing d must start a fetch for the now-selected object.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m.detailsLoading {
		t.Fatal("d after sort should start a fetch for the row under the cursor")
	}

	// And the arriving result must be shown for that object.
	det := &ObjectDetails{ContentType: "application/json"}
	m.Update(objectDetailsMsg{key: "logs/b.txt", details: det})
	if m.selectedDetails != det {
		t.Errorf("fetched details should be shown after a sort, got %v", m.selectedDetails)
	}
}

// A failed metadata fetch (e.g. denied HeadObject) must surface the error and
// stay retryable — not silently revert to the "press d" prompt.
func TestDetailsFetchErrorIsSurfacedAndRetryable(t *testing.T) {
	m := newObjectListModel() // cursor on a.txt

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	m.Update(objectDetailsMsg{key: "logs/a.txt", err: fmt.Errorf("HeadObject: AccessDenied")})

	if m.detailsErr == nil {
		t.Fatal("a fetch error should be recorded so it can be shown")
	}
	if m.selectedDetails != nil {
		t.Error("no details should be shown on error")
	}
	if _, cached := m.objectDetailsCache["logs/a.txt"]; cached {
		t.Error("a failed fetch must not be cached (so d can retry)")
	}

	// Retry: d fetches again and clears the prior error.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !m.detailsLoading || m.detailsErr != nil {
		t.Errorf("retry should re-fetch and clear the error: loading=%v err=%v", m.detailsLoading, m.detailsErr)
	}
}
