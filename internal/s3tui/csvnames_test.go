package s3tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseNamesLayout(t *testing.T) {
	names, err := parseNamesLayout("# customer columns\nid\n\nname\ncity\n")
	if err != nil {
		t.Fatalf("parseNamesLayout error: %v", err)
	}
	if strings.Join(names, ",") != "id,name,city" {
		t.Errorf("names = %v", names)
	}

	if _, err := parseNamesLayout("id\nname,11,30\n"); err == nil {
		t.Error("mixed name-only and fixed-width lines must be rejected")
	}
	if _, err := parseNamesLayout("# only comments\n\n"); err == nil {
		t.Error("empty names layout must be rejected")
	}
}

func TestLayoutSpecIsNamesOnly(t *testing.T) {
	if !layoutSpecIsNamesOnly("# comment\nid\nname\n") {
		t.Error("bare names should be detected as names-only")
	}
	if layoutSpecIsNamesOnly("# comment\naccount_id,1,10\n") {
		t.Error("name,start,length lines are a fixed-width layout")
	}
	if layoutSpecIsNamesOnly("") {
		t.Error("an empty spec is not a names layout")
	}
}

// The dummy-first-record case: header row 1 (the default) is skipped without
// being read, and the layout supplies the titles.
func TestNamesLayoutSkipsDummyRow(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("DUMMY,DUMMY,DUMMY\n1,alice,sydney\n2,bob,perth\n") {
		t.Fatal("initCSV should parse")
	}
	m.csvNames = []string{"id", "name", "city"}
	m.buildCSVTable()

	header, data := m.headerAndData()
	if strings.Join(header, ",") != "id,name,city" {
		t.Errorf("header = %v, want the layout names", header)
	}
	if len(data) != 2 || data[0][1] != "alice" {
		t.Errorf("data = %v, want the dummy row skipped and alice first", data)
	}
	view := m.csvTable.View()
	for _, want := range []string{"id", "name", "city", "alice"} {
		if !strings.Contains(view, want) {
			t.Errorf("table view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "DUMMY") {
		t.Errorf("dummy row must not appear as data or titles:\n%s", view)
	}
}

// Header row 0 + names: nothing is skipped, the names still title the columns.
func TestNamesLayoutNoHeaderRow(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("1,alice\n2,bob\n") {
		t.Fatal("initCSV should parse")
	}
	m.csvNames = []string{"id", "name"}
	m.csvHeaderRow = 0
	m.buildCSVTable()

	header, data := m.headerAndData()
	if strings.Join(header, ",") != "id,name" {
		t.Errorf("header = %v", header)
	}
	if len(data) != 2 {
		t.Errorf("all %d rows should be data, want 2", len(data))
	}
}

// Name/column count mismatches are visible: missing names synthesise, extra
// names become (empty) columns, and the info line states the mismatch.
func TestNamesLayoutCountMismatch(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("x,x,x\n1,alice,sydney\n") {
		t.Fatal("initCSV should parse")
	}

	m.csvNames = []string{"id", "name"} // one short
	m.buildCSVTable()
	header, _ := m.headerAndData()
	if strings.Join(header, ",") != "id,name,col 3" {
		t.Errorf("short names: header = %v", header)
	}
	if info := m.csvInfoLine(); !strings.Contains(info, "2 names for 3 data columns") {
		t.Errorf("info line should state the mismatch: %q", info)
	}

	m.csvNames = []string{"id", "name", "city", "extra"} // one long
	m.buildCSVTable()
	header, _ = m.headerAndData()
	if strings.Join(header, ",") != "id,name,city,extra" {
		t.Errorf("long names: header = %v", header)
	}
}

// The full prompt path: a names-only file routes to the delimited preview and
// a fixed-width file still routes to the positional table.
func TestApplyLayoutPromptRoutesNamesLayout(t *testing.T) {
	dir := t.TempDir()
	namesPath := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(namesPath, []byte("# names\nid\nname\ncity\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Model{width: 80, height: 20, showCSV: true}
	m.previewContent = "DUMMY,DUMMY,DUMMY\n1,alice,sydney\n"
	if !m.initCSV(m.previewContent) {
		t.Fatal("initCSV should parse")
	}
	m.startLayoutPrompt()
	m.layoutInput.SetValue(namesPath)
	m.applyLayoutPrompt()

	if m.layoutErr != "" {
		t.Fatalf("layoutErr = %q", m.layoutErr)
	}
	if m.enteringLayout {
		t.Error("prompt should close on success")
	}
	if strings.Join(m.csvNames, ",") != "id,name,city" {
		t.Errorf("csvNames = %v", m.csvNames)
	}
	if m.previewIsFixed {
		t.Error("a names layout must not switch to the fixed-width preview")
	}
	if info := m.csvInfoLine(); !strings.Contains(info, "names: layout (rows 1-1 skipped)") {
		t.Errorf("info line = %q", info)
	}
}

// Names layouts are rejected where they can't apply, with the prompt kept open.
func TestApplyNamesLayoutRejectedForTypedPreviews(t *testing.T) {
	dir := t.TempDir()
	namesPath := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(namesPath, []byte("id\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("a,b\n1,2\n") {
		t.Fatal("initCSV should parse")
	}
	m.previewIsParquet = true
	m.startLayoutPrompt()
	m.layoutInput.SetValue(namesPath)
	m.applyLayoutPrompt()
	if m.layoutErr == "" || !m.enteringLayout {
		t.Error("names layout over Parquet should error and keep the prompt open")
	}

	m.previewIsParquet = false
	m.previewIsFixed = true
	m.applyLayoutPrompt()
	if m.layoutErr == "" {
		t.Error("names layout over a fixed-width preview should error")
	}
}

// Opening the next file must not leak the previous file's names.
func TestNamesLayoutResetPerFile(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("x,x\n1,2\n") {
		t.Fatal("initCSV should parse")
	}
	m.csvNames = []string{"id", "qty"}
	if !m.initCSV("h1,h2\n3,4\n") {
		t.Fatal("second initCSV should parse")
	}
	if m.csvNames != nil {
		t.Errorf("csvNames should reset per file, got %v", m.csvNames)
	}
}
