package s3tui

import (
	"strings"
	"testing"
)

// usTable joins rows into unit-separated lines, the way a mainframe extract
// lays them out.
func usTable(rows ...[]string) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Join(r, "\x1f") + "\n")
	}
	return b.String()
}

// A unit-separated file is a table whatever it is called: the extension is a
// hint, the content is the format.
func TestUnitSeparatedOpensAsTableWhateverTheKey(t *testing.T) {
	content := usTable([]string{"id", "name"}, []string{"1", "bob"}, []string{"2", "al"})
	for _, key := range []string{"extract.dat", "extract.txt", "extract", "part-00000", "extract.dat.20260101"} {
		m := &Model{width: 120, height: 30, state: stateObjectList, focus: focusObjects}
		m.openPreview(key)
		m.Update(objectPreviewMsg{key: key, content: content})
		if !m.showCSV || m.showPreview {
			t.Errorf("%s: want the table view, got showCSV=%v showPreview=%v", key, m.showCSV, m.showPreview)
			continue
		}
		if m.csvDelim != unitSep || len(m.csvAll) != 3 {
			t.Errorf("%s: delim=%q rows=%d, want unit separator and 3 rows", key, m.csvDelim, len(m.csvAll))
		}
	}
}

// Ordinary text is not pulled into the table view by the content check.
func TestPlainTextStaysInTextPreview(t *testing.T) {
	for _, content := range []string{
		"a log line, with commas\nanother, line\n",
		// One stray separator in a long file is not a table.
		"line\nline\nline\nwith\x1fone\nline\nline\n",
	} {
		if looksUnitSeparated(content) {
			t.Errorf("treated as unit-separated: %q", content)
		}
	}
}

// Unit-separated fields are literal: a leading double quote is text, not the
// start of a quoted field that swallows the rows after it.
func TestUnitSeparatedQuotesAreLiteral(t *testing.T) {
	content := usTable(
		[]string{"id", "name", "amt"},
		[]string{"1", `"Bob" Smith`, "10"},
		[]string{"2", `"5 inch`, "20"},
		[]string{"3", "cy", "30"},
	)
	recs, ok := parseCSV(content, detectDelimiter(content))
	if !ok || len(recs) != 4 {
		t.Fatalf("ok=%v rows=%d, want 4 rows: %q", ok, len(recs), recs)
	}
	if recs[1][1] != `"Bob" Smith` || recs[2][1] != `"5 inch` || recs[3][2] != "30" {
		t.Errorf("fields altered: %q", recs)
	}
}

// Commas or pipes inside the text fields must not outvote the real delimiter.
func TestUnitSeparatorWinsOverPunctuationInFields(t *testing.T) {
	for _, content := range []string{
		usTable([]string{"id", "desc"}, []string{"1", "a, b, c, d"}, []string{"2", "e, f, g, h"}),
		usTable([]string{"id", "desc"}, []string{"1", "x|y|z"}, []string{"2", "x|y|z"}),
	} {
		if d := detectDelimiter(content); d != unitSep {
			t.Errorf("detected %s for %q", delimiterName(d), content)
		}
		if recs, ok := parseCSV(content, detectDelimiter(content)); !ok || len(recs[0]) != 2 {
			t.Errorf("not parsed as 2 columns: ok=%v %q", ok, recs)
		}
	}
	// A real CSV is still a CSV.
	if d := detectDelimiter("a,b,c\n1,2,3\n"); d != ',' {
		t.Errorf("comma file detected as %s", delimiterName(d))
	}
}

// A single record is still a table, shown as data under synthesised column
// names rather than as a header with nothing beneath it.
func TestUnitSeparatedSingleRecord(t *testing.T) {
	m := &Model{width: 120, height: 30}
	if !m.initCSV("1\x1fbob\x1f10\n") {
		t.Fatal("single record not tabulated")
	}
	header, data := m.headerAndData()
	if len(data) != 1 || data[0][1] != "bob" || header[0] != "col 1" {
		t.Errorf("header=%q data=%q", header, data)
	}
}

// Records may be split by the ASCII record separator instead of newlines,
// with or without a newline after each.
func TestUnitSeparatedRecordSeparator(t *testing.T) {
	for _, content := range []string{
		"id\x1fname\x1e1\x1fbob\x1e2\x1fal\x1e",
		"id\x1fname\x1e\n1\x1fbob\x1e\n2\x1fal\x1e\n",
	} {
		recs, ok := parseCSV(content, detectDelimiter(content))
		if !ok || len(recs) != 3 || recs[2][1] != "al" {
			t.Errorf("ok=%v recs=%q", ok, recs)
		}
	}
}

// CRLF line endings don't leave a carriage return on the last field.
func TestUnitSeparatedCRLF(t *testing.T) {
	recs, ok := parseCSV("id\x1fname\r\n1\x1fbob\r\n", unitSep)
	if !ok || recs[1][1] != "bob" {
		t.Errorf("ok=%v recs=%q", ok, recs)
	}
}

// The raw view keeps the columns apart: the separators show as control
// pictures instead of being stripped, which ran the fields together.
func TestRawViewShowsSeparators(t *testing.T) {
	if got := sanitizeForDisplay("a\x1fb\x1ec\n"); got != "a␟b␞c\n" {
		t.Errorf("got %q", got)
	}
}

// A compressed or archived unit-separated member is not given a truncation
// note, which would become a junk last row.
func TestUnitSeparatedArchiveMemberIsTable(t *testing.T) {
	content := usTable([]string{"id", "name"}, []string{"1", "bob"})
	if !looksDelimited("member.txt", content) || looksDelimited("member.txt", "plain text\n") {
		t.Error("looksDelimited ignores the content")
	}
	if got := decompressedPreview([]byte(content), true, looksDelimited("member.txt", content)); got != content {
		t.Errorf("truncation note added to a table: %q", got)
	}
}
