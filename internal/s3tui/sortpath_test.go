package s3tui

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func newTestPrefixInput(value string) textinput.Model {
	in := textinput.New()
	in.SetValue(value)
	return in
}

func dirRow(name string) map[string]string {
	return map[string]string{"name": name, "type": "DIR", "size": "-", "last_modified": "-", "storage_class": "DIR", "etag": "-"}
}

// R must visibly reverse a folder-only listing: directories order by name in
// the active direction (they have no size/date), instead of being stuck
// ascending while the files below them flip.
func TestSortReversesDirectories(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusObjects,
		objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.objectMaps = []map[string]string{
		dirRow(".."), dirRow("alpha/"), dirRow("bravo/"), dirRow("charlie/"),
		{"name": "a.txt", "type": "FILE", "size": "1 B", "last_modified": "2026-01-01 00:00:00", "storage_class": "STANDARD", "etag": "e"},
	}
	m.sortAsc = true
	m.sortObjects(m.objectMaps)
	m.objectTable.SetRows(m.buildObjectRows())

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})

	wantNames := []string{"..", "charlie/", "bravo/", "alpha/", "a.txt"}
	for i, want := range wantNames {
		if got := m.objectMaps[i]["name"]; got != want {
			t.Fatalf("after R, row %d = %q, want %q (maps=%v)", i, got, want, m.objectMaps)
		}
	}

	// And R again restores ascending, ".." still pinned first.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})
	wantNames = []string{"..", "alpha/", "bravo/", "charlie/", "a.txt"}
	for i, want := range wantNames {
		if got := m.objectMaps[i]["name"]; got != want {
			t.Fatalf("after 2nd R, row %d = %q, want %q", i, got, want)
		}
	}
}

// The full loaded listing — not just the visible page — must reverse on R.
func TestSortReversesEntireLargeListing(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusObjects,
		objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	const n = 4197
	for i := range n {
		m.objectMaps = append(m.objectMaps, map[string]string{
			"name": fmt.Sprintf("file-%05d.txt", i), "type": "FILE", "size": "1 B",
			"last_modified": "2026-01-01 00:00:00", "storage_class": "STANDARD", "etag": fmt.Sprintf("e%d", i),
		})
	}
	m.sortAsc = true
	m.sortObjects(m.objectMaps)
	m.objectTable.SetRows(m.buildObjectRows())

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")})

	rows := m.objectTable.Rows()
	if len(rows) != n {
		t.Fatalf("rows = %d, want %d", len(rows), n)
	}
	if rows[0][1] != fmt.Sprintf("file-%05d.txt", n-1) {
		t.Errorf("after R, first row = %q, want the last file", rows[0][1])
	}
	if rows[n-1][1] != "file-00000.txt" {
		t.Errorf("after R, last row = %q, want file-00000.txt", rows[n-1][1])
	}
}

// Entering a full object path in the "/" prompt must land on that object:
// the optimistic folder listing comes back empty, the parent folder is
// listed instead, and the cursor selects the object.
func TestPrefixInputFullObjectPath(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusPrefixInput,
		bucket: "b", objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.prefixInput = newTestPrefixInput("logs/2026/report.csv")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.prefix != "logs/2026/report.csv/" {
		t.Fatalf("enter should first try the path as a folder, prefix = %q", m.prefix)
	}
	if m.objectPathFallback != "logs/2026/report.csv" {
		t.Fatalf("object-path fallback not armed: %q", m.objectPathFallback)
	}

	// The folder listing is empty (only the up-dir row) → fall back.
	_, cmd := m.Update(objectsLoadedMsg{maps: []map[string]string{dirRow("..")}})
	if m.prefix != "logs/2026/" {
		t.Fatalf("empty listing should retry the parent folder, prefix = %q", m.prefix)
	}
	if m.objectJumpTarget != "report.csv" {
		t.Fatalf("jump target = %q, want report.csv", m.objectJumpTarget)
	}
	if cmd == nil {
		t.Fatal("the fallback must issue a reload of the parent folder")
	}

	// The parent listing arrives → the object is selected.
	m.Update(objectsLoadedMsg{maps: []map[string]string{
		dirRow(".."),
		{"name": "archive.csv", "type": "FILE", "size": "1 B", "last_modified": "2026-01-01 00:00:00", "storage_class": "STANDARD", "etag": "e1"},
		{"name": "report.csv", "type": "FILE", "size": "2 B", "last_modified": "2026-01-02 00:00:00", "storage_class": "STANDARD", "etag": "e2"},
	}, count: 2, size: 3})
	if m.objectJumpTarget != "" {
		t.Error("jump target should be consumed")
	}
	if key, ok := m.selectedObjectKey(); !ok || key != "logs/2026/report.csv" {
		t.Errorf("selected key = %q (%v), want logs/2026/report.csv", key, ok)
	}
}

// A path that is neither a folder nor an object surfaces a not-found note
// instead of a silent empty screen.
func TestPrefixInputPathNotFound(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusPrefixInput,
		bucket: "b", objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.prefixInput = newTestPrefixInput("logs/nope.txt")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(objectsLoadedMsg{maps: nil}) // folder try: empty
	m.Update(objectsLoadedMsg{maps: []map[string]string{
		dirRow(".."),
		{"name": "real.txt", "type": "FILE", "size": "1 B", "last_modified": "2026-01-01 00:00:00", "storage_class": "STANDARD", "etag": "e"},
	}, count: 1, size: 1})

	if m.statusMsg == "" {
		t.Error("a missing path should surface a not-found status message")
	}
	if m.prefix != "logs/" {
		t.Errorf("prefix = %q, want the parent folder logs/", m.prefix)
	}
}

// A folder entered without its trailing slash keeps the old behavior: the
// listing is non-empty, so no fallback reload happens.
func TestPrefixInputFolderWithoutSlashUnchanged(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusPrefixInput,
		bucket: "b", objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.prefixInput = newTestPrefixInput("logs/2026")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.prefix != "logs/2026/" {
		t.Fatalf("prefix = %q, want logs/2026/", m.prefix)
	}

	_, cmd := m.Update(objectsLoadedMsg{maps: []map[string]string{
		dirRow(".."),
		{"name": "a.txt", "type": "FILE", "size": "1 B", "last_modified": "2026-01-01 00:00:00", "storage_class": "STANDARD", "etag": "e"},
	}, count: 1, size: 1})
	if m.prefix != "logs/2026/" {
		t.Errorf("non-empty folder must not trigger the fallback, prefix = %q", m.prefix)
	}
	if m.objectJumpTarget != "" || m.objectPathFallback != "" {
		t.Error("fallback state should be cleared after a successful folder listing")
	}
	_ = cmd
}

// Flat mode treats the input as a free-form key filter: no slash is forced,
// so a full object path matches its exact key.
func TestPrefixInputFlatModeKeepsExactPath(t *testing.T) {
	m := &Model{width: 120, height: 40, state: stateObjectList, focus: focusPrefixInput,
		bucket: "b", flatMode: true, objectDetailsCache: map[string]*ObjectDetails{}}
	m.initObjectTable()
	m.prefixInput = newTestPrefixInput("logs/2026/report.csv")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.prefix != "logs/2026/report.csv" {
		t.Fatalf("flat mode must not append a slash, prefix = %q", m.prefix)
	}
	if m.objectPathFallback != "" {
		t.Error("flat mode needs no object-path fallback")
	}
}

// A flat-mode prefix naming an exact key keeps that object visible (named by
// its final segment) while a folder-marker key stays hidden.
func TestBuildObjectMapsExactKeyPrefix(t *testing.T) {
	res := &ListObjectsResult{Objects: []s3types.Object{
		{Key: aws.String("logs/2026/report.csv"), Size: aws.Int64(10)},
	}}
	maps, count, _ := buildObjectMaps(res, "logs/2026/report.csv", true, false)
	if count != 1 || len(maps) != 1 {
		t.Fatalf("exact-key prefix lost the object: count=%d maps=%v", count, maps)
	}
	if maps[0]["name"] != "report.csv" {
		t.Errorf("name = %q, want report.csv", maps[0]["name"])
	}

	// Folder markers (the "logs/" placeholder of the listed folder) stay hidden.
	res = &ListObjectsResult{Objects: []s3types.Object{{Key: aws.String("logs/")}}}
	maps, count, _ = buildObjectMaps(res, "logs/", false, false)
	if count != 0 || len(maps) != 0 {
		t.Errorf("folder marker should be skipped: count=%d maps=%v", count, maps)
	}
}
