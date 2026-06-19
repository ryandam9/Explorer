package s3tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestCSVRecordView(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("id,name,city\n1,alice,sydney\n2,bob,perth\n") {
		t.Fatal("initCSV should parse")
	}
	m.csvTable.SetCursor(1) // the "bob" row
	m.openCSVRecord()

	if !m.csvRecordActive || m.csvRecordIndex != 1 {
		t.Fatalf("record not opened: active=%v idx=%d", m.csvRecordActive, m.csvRecordIndex)
	}
	view := m.csvRecordViewport.View()
	for _, want := range []string{"name", "city", "bob", "perth"} {
		if !strings.Contains(view, want) {
			t.Errorf("record view missing %q:\n%s", want, view)
		}
	}
}

// Each line in the vertical record view is prefixed with its column number.
func TestCSVRecordSequenceNumbers(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("id,name,city\n1,alice,sydney\n2,bob,perth\n") {
		t.Fatal("initCSV should parse")
	}
	m.csvTable.SetCursor(0) // the "alice" row
	m.openCSVRecord()

	view := m.csvRecordViewport.View()
	for _, want := range []string{"1: id", "2: name", "3: city"} {
		if !strings.Contains(view, want) {
			t.Errorf("record view missing column number %q:\n%s", want, view)
		}
	}
}

func TestCSVRecordSkipsDivider(t *testing.T) {
	var b strings.Builder
	b.WriteString("a,b\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "%d,x\n", i)
	}
	// cap 2 → first 2 + divider(nil) + last 2; the divider is display index 2.
	m := &Model{width: 80, height: 20, showCSV: true, csvRowCap: 2, csvRowCapSet: true}
	if !m.initCSV(b.String()) {
		t.Fatal("initCSV should parse")
	}
	m.csvTable.SetCursor(2)
	m.openCSVRecord()
	if m.csvRecordActive {
		t.Error("opening the elision divider row should be a no-op")
	}
}

// Synthesised column names (no header) appear in the record view.
func TestCSVRecordNoHeader(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	m.initCSV("1,alice\n2,bob\n")
	m.csvHeaderRow = 0
	m.buildCSVTable()
	m.csvTable.SetCursor(0)
	m.openCSVRecord()
	if v := m.csvRecordViewport.View(); !strings.Contains(v, "col 1") || !strings.Contains(v, "alice") {
		t.Errorf("no-header record should use synthesised names:\n%s", v)
	}
}
