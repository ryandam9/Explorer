package s3tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestLooksLikeCSV(t *testing.T) {
	for _, k := range []string{"data.csv", "DATA.CSV", "report.tsv", "a/b/c.tab"} {
		if !looksLikeCSV(k) {
			t.Errorf("%q should look like CSV", k)
		}
	}
	for _, k := range []string{"notes.txt", "image.png", "archive.csv.gz", "data"} {
		if looksLikeCSV(k) {
			t.Errorf("%q should not look like CSV", k)
		}
	}
}

func TestDetectDelimiter(t *testing.T) {
	cases := map[string]rune{
		"a,b,c\n1,2,3\n4,5,6":       ',',
		"a\tb\tc\n1\t2\t3\n4\t5\t6": '\t',
		"a;b;c\n1;2;3\n4;5;6":       ';',
		"a|b|c\n1|2|3":              '|',
	}
	for content, want := range cases {
		if got := detectDelimiter(content); got != want {
			t.Errorf("detectDelimiter(%q) = %q, want %q", content, string(got), string(want))
		}
	}
}

func TestParseCSV(t *testing.T) {
	recs, ok := parseCSV("id,name\n1,alice\n2,bob", ',')
	if !ok {
		t.Fatal("expected table-shaped content to parse")
	}
	if len(recs) != 3 || recs[0][1] != "name" || recs[2][1] != "bob" {
		t.Errorf("unexpected parse: %v", recs)
	}
	// Quoted field containing the delimiter and a newline is handled.
	recs, ok = parseCSV("a,b\n\"x,y\",\"line1\nline2\"", ',')
	if !ok || recs[1][0] != "x,y" {
		t.Errorf("quoted field mishandled: %v ok=%v", recs, ok)
	}
	// Single-column / plain text is not table-shaped.
	if _, ok := parseCSV("just one column\nanother line", ','); ok {
		t.Error("single-column content should not parse as a table")
	}
	// A truncated final row keeps the complete rows parsed before it.
	recs, ok = parseCSV("a,b,c\n1,2,3\n4,\"unterminated", ',')
	if !ok || len(recs) < 2 {
		t.Errorf("truncated tail should keep good rows: %v ok=%v", recs, ok)
	}
}

func TestWindowRecords(t *testing.T) {
	data := make([][]string, 250)
	for i := range data {
		data[i] = []string{fmt.Sprintf("%d", i)}
	}
	// cap 100 → first 100 + divider(nil) + last 100, 50 hidden.
	display, hidden := windowRecords(data, 100)
	if hidden != 50 {
		t.Errorf("hidden = %d, want 50", hidden)
	}
	if len(display) != 201 {
		t.Errorf("display len = %d, want 201 (100+divider+100)", len(display))
	}
	if display[100] != nil {
		t.Errorf("expected a nil divider at the boundary, got %v", display[100])
	}
	if display[0][0] != "0" || display[len(display)-1][0] != "249" {
		t.Errorf("window endpoints wrong: %v … %v", display[0], display[len(display)-1])
	}
	// cap 0 → everything, no divider.
	if d, h := windowRecords(data, 0); h != 0 || len(d) != 250 {
		t.Errorf("cap 0 should show all: len=%d hidden=%d", len(d), h)
	}
	// Small data under 2×cap is shown whole.
	if d, h := windowRecords(data[:150], 100); h != 0 || len(d) != 150 {
		t.Errorf("150 rows under 2×100 should show whole: len=%d hidden=%d", len(d), h)
	}
}

func TestClipCell(t *testing.T) {
	if got := clipCell("line1\nline2\ttab"); strings.ContainsAny(got, "\n\t") {
		t.Errorf("clipCell should flatten whitespace: %q", got)
	}
	long := strings.Repeat("x", 200)
	if got := clipCell(long); len([]rune(got)) != csvCellCap {
		t.Errorf("clipCell len = %d, want %d", len([]rune(got)), csvCellCap)
	}
}

func TestInitCSVAndWindowing(t *testing.T) {
	var b strings.Builder
	b.WriteString("id,name\n")
	for i := 1; i <= 250; i++ {
		fmt.Fprintf(&b, "%d,User %d\n", i, i)
	}
	m := &Model{width: 100, height: 30}
	if !m.initCSV(b.String()) {
		t.Fatal("initCSV should succeed for a real CSV")
	}
	if m.csvDelim != ',' {
		t.Errorf("delim = %q", string(m.csvDelim))
	}
	if m.csvTotal != 250 {
		t.Errorf("total = %d, want 250", m.csvTotal)
	}
	if m.csvRowCap != defaultCSVRowCap || m.csvHidden != 50 {
		t.Errorf("cap=%d hidden=%d, want %d/50", m.csvRowCap, m.csvHidden, defaultCSVRowCap)
	}
	// Cycling the window to "all" shows everything.
	for m.csvRowCap != 0 {
		m.cycleCSVRowCap()
	}
	if m.csvHidden != 0 {
		t.Errorf("after cap=all, hidden = %d, want 0", m.csvHidden)
	}
}
