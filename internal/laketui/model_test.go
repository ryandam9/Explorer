package laketui

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/traillake"
)

func testResult() traillake.Result {
	return traillake.Result{
		QueryID: "q-1",
		Columns: []string{"eventName", "events"},
		Rows: [][]string{
			{"RunInstances", "9"},
			{"DeleteBucket", "12"},
			{"CreateUser", "100"},
		},
		BytesScanned: 2048,
	}
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	m := New(context.Background(), aws.Config{}, "SELECT …", traillake.QueryOptions{}, "top events", "audit-store", "us-east-1")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	mm, _ = m.Update(loadedMsg{result: testResult()})
	return mm.(Model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestLoadBuildsGenericTable(t *testing.T) {
	m := newTestModel(t)
	if m.loading {
		t.Error("loading should be false after loadedMsg")
	}
	if len(m.visible) != 3 {
		t.Fatalf("visible = %d, want 3", len(m.visible))
	}
	out := m.View()
	for _, want := range []string{"EVENTNAME", "EVENTS", "RunInstances", "CreateUser"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestNumericAwareSort(t *testing.T) {
	m := newTestModel(t)
	// Cycle sort to column index 1 ("events"): s once -> col 0, s again -> col 1.
	m = update(m, key("s")) // sortCol 0 (eventName)
	m = update(m, key("s")) // sortCol 1 (events), ascending
	if m.sortCol != 1 {
		t.Fatalf("sortCol = %d, want 1", m.sortCol)
	}
	// Ascending numeric: 9, 12, 100 (not lexical 100, 12, 9).
	if m.visible[0][1] != "9" || m.visible[1][1] != "12" || m.visible[2][1] != "100" {
		t.Errorf("numeric sort wrong: %v", m.visible)
	}
}

func TestFilterRows(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("/"))
	for _, r := range "bucket" {
		m = update(m, key(string(r)))
	}
	if len(m.visible) != 1 || m.visible[0][0] != "DeleteBucket" {
		t.Errorf("filter 'bucket' should match one row, got %v", m.visible)
	}
}

func TestDetailOverlayShowsColumns(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("enter"))
	if m.overlay != overlayDetail {
		t.Fatal("enter should open detail")
	}
	out := m.View()
	if !strings.Contains(out, "eventName") || !strings.Contains(out, "events") {
		t.Errorf("detail overlay should list column names:\n%s", out)
	}
}

// captureClipboard redirects the clipboard sink to a buffer for the duration of
// a test, since the real clipboard is unavailable in headless CI.
func captureClipboard(t *testing.T) *string {
	t.Helper()
	var got string
	prev := clipboardWrite
	clipboardWrite = func(s string) error { got = s; return nil }
	t.Cleanup(func() { clipboardWrite = prev })
	return &got
}

func TestCopyDetailOverlayCopiesPanel(t *testing.T) {
	got := captureClipboard(t)
	m := newTestModel(t)
	m = update(m, key("enter")) // open detail for the first row (RunInstances)
	m = update(m, key("y"))

	if m.status != "copied row detail" {
		t.Errorf("copying with the overlay open should report the detail copy, got %q", m.status)
	}
	// The labelled panel carries each column name and value, one per line.
	for _, want := range []string{"eventName: RunInstances", "events: 9"} {
		if !strings.Contains(*got, want) {
			t.Errorf("clipboard missing %q:\n%s", want, *got)
		}
	}
	if strings.Contains(*got, "\t") {
		t.Errorf("overlay copy should be labelled lines, not tab-separated:\n%s", *got)
	}
}

func TestCopyRowWithoutOverlay(t *testing.T) {
	got := captureClipboard(t)
	m := newTestModel(t)
	m = update(m, key("y"))

	if m.status != "copied row" {
		t.Errorf("copying without the overlay should copy the raw row, got %q", m.status)
	}
	if *got != "RunInstances\t9" {
		t.Errorf("row copy should be tab-separated, got %q", *got)
	}
}

func TestLoadErrorBody(t *testing.T) {
	m := New(context.Background(), aws.Config{}, "SELECT …", traillake.QueryOptions{}, "recent", "store", "us-east-1")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(loadedMsg{err: errString("query timed out")})
	m = mm.(Model)
	if !strings.Contains(m.View(), "query timed out") {
		t.Errorf("load error should surface in the body:\n%s", m.View())
	}
}

func TestLessCellNumericVsString(t *testing.T) {
	if !lessCell("9", "100") {
		t.Error("9 should sort before 100 numerically")
	}
	if lessCell("banana", "apple") {
		t.Error("string fallback should order apple before banana")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
