package s3tui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseLayout(t *testing.T) {
	spec := "# customer record\n" +
		"id,1,5\n" +
		"name, 6 , 10\n" +
		"\n" +
		"amount,16,8\n"
	fields, err := parseLayout(spec)
	if err != nil {
		t.Fatalf("parseLayout: %v", err)
	}
	want := []fixedField{
		{name: "id", start: 1, length: 5},
		{name: "name", start: 6, length: 10},
		{name: "amount", start: 16, length: 8},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("parseLayout = %+v, want %+v", fields, want)
	}
}

func TestParseLayoutErrors(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"comments only":  "# nothing here\n\n",
		"too few fields": "id,1\n",
		"bad start":      "id,x,5\n",
		"zero start":     "id,0,5\n",
		"bad length":     "id,1,-3\n",
		"empty name":     ",1,5\n",
	}
	for name, spec := range cases {
		if _, err := parseLayout(spec); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
}

func TestBuildFixedRecords(t *testing.T) {
	fields := []fixedField{
		{name: "id", start: 1, length: 3},
		{name: "name", start: 4, length: 6},
		{name: "amt", start: 10, length: 4},
	}
	// total width = 13. Row 2 is short (malformed), row 3 is long (malformed).
	content := "001alice 0100\n" +
		"002bob\n" +
		"003carol 9999EXTRA\n"
	recs, bad := buildFixedRecords(content, fields)

	wantHeader := []string{"!", "id", "name", "amt"}
	if !reflect.DeepEqual(recs[0], wantHeader) {
		t.Fatalf("header = %v, want %v", recs[0], wantHeader)
	}
	if got := recs[1]; !reflect.DeepEqual(got, []string{"", "001", "alice", "0100"}) {
		t.Errorf("row 1 = %v", got)
	}
	// Short line: trailing column is blank, row flagged.
	if got := recs[2]; !reflect.DeepEqual(got, []string{"!", "002", "bob", ""}) {
		t.Errorf("row 2 = %v", got)
	}
	// Long line: data sliced at fixed positions, row flagged.
	if got := recs[3]; !reflect.DeepEqual(got, []string{"!", "003", "carol", "9999"}) {
		t.Errorf("row 3 = %v", got)
	}
	if bad != 2 {
		t.Errorf("badRows = %d, want 2", bad)
	}
}

// When the fixed-width table is scrolled wider than the view, the "cols X-Y of
// N" info line must count only data columns — the leading "!" marker column is
// not a data column and must not inflate the total or shift the numbers.
func TestFixedInfoLineExcludesMarkerColumn(t *testing.T) {
	fields := []fixedField{
		{name: "alpha", start: 1, length: 12},
		{name: "bravo", start: 13, length: 12},
		{name: "charlie", start: 25, length: 12},
		{name: "delta", start: 37, length: 12},
		{name: "echo", start: 49, length: 12},
	}
	content := "alphavalue01bravovalue01charlievalu1deltavalue01echovalue001\n" +
		"alphavalue02bravovalue02charlievalu2deltavalue02echovalue002\n"
	recs, bad := buildFixedRecords(content, fields)

	m := &Model{width: 40, height: 20}
	m.initFixed(recs, fields, bad)
	m.csvTable.ScrollRight() // hide a leading data column so the window info shows

	info := m.csvInfoLine()
	// Five data columns, not six — the "!" marker must not be counted.
	if !strings.Contains(info, "of 5") {
		t.Errorf("info line should report 5 data columns: %q", info)
	}
	if strings.Contains(info, "of 6") || strings.Contains(info, "col 6:") {
		t.Errorf("info line should not count the marker column: %q", info)
	}
}

// Fixed-width columns carry their layout positions: the numbering line under
// the headers reads "(1) 1-5", and the record view shows "[1-5]" per field.
func TestFixedColumnPositions(t *testing.T) {
	fields := []fixedField{
		{name: "id", start: 1, length: 5},
		{name: "name", start: 6, length: 10},
	}
	recs, bad := buildFixedRecords("00001alice     \n", fields)

	m := &Model{width: 80, height: 20, showCSV: true}
	m.initFixed(recs, fields, bad)

	cols := m.csvTable.Columns()
	if cols[0].Note != "" {
		t.Errorf("marker column must have no position note, got %q", cols[0].Note)
	}
	if cols[1].Note != "1-5" || cols[2].Note != "6-15" {
		t.Errorf("position notes = %q, %q; want 1-5, 6-15", cols[1].Note, cols[2].Note)
	}
	for _, want := range []string{"(1) 1-5", "(2) 6-15"} {
		if v := m.csvTable.View(); !strings.Contains(v, want) {
			t.Errorf("table numbering line missing %q:\n%s", want, v)
		}
	}

	m.csvTable.SetCursor(0)
	m.openCSVRecord()
	v := m.csvRecordViewport.View()
	for _, want := range []string{"[1-5]", "[6-15]", "alice"} {
		if !strings.Contains(v, want) {
			t.Errorf("record view missing %q:\n%s", want, v)
		}
	}
}

// A non-fixed preview must not gain position notes or the record positions
// column.
func TestCSVHasNoPositionNotes(t *testing.T) {
	m := &Model{width: 80, height: 20, showCSV: true}
	if !m.initCSV("id,name\n1,alice\n") {
		t.Fatal("initCSV should parse")
	}
	for _, c := range m.csvTable.Columns() {
		if c.Note != "" {
			t.Errorf("CSV column %q unexpectedly has note %q", c.Title, c.Note)
		}
	}
	m.csvTable.SetCursor(0)
	m.openCSVRecord()
	if v := m.csvRecordViewport.View(); strings.Contains(v, "[1-") {
		t.Errorf("CSV record view should have no position column:\n%s", v)
	}
}

func TestBuildFixedRecordsStripsBOM(t *testing.T) {
	fields := []fixedField{{name: "a", start: 1, length: 2}}
	recs, _ := buildFixedRecords("\ufeffXY\n", fields)
	if recs[1][1] != "XY" {
		t.Errorf("BOM not stripped: got %q, want %q", recs[1][1], "XY")
	}
}
