package s3tui

import (
	"strings"
	"testing"
)

func TestCSVMarkdown(t *testing.T) {
	header := []string{"name", "city", ""}
	rows := [][]string{
		{"alice", "syd|ney", "x"},
		nil, // window divider — must be skipped
		{"bob", "line1\nline2", "y\tz"},
	}
	md, n := csvMarkdown(header, rows)
	if n != 2 {
		t.Errorf("row count = %d, want 2 (divider skipped)", n)
	}
	lines := strings.Split(strings.TrimRight(md, "\n"), "\n")
	if len(lines) != 4 { // header + separator + 2 data rows
		t.Fatalf("expected 4 lines, got %d:\n%s", len(lines), md)
	}
	if lines[0] != "| name | city | col 3 |" {
		t.Errorf("header line = %q (empty header should become col 3)", lines[0])
	}
	if lines[1] != "| --- | --- | --- |" {
		t.Errorf("separator line = %q", lines[1])
	}
	if !strings.Contains(lines[2], `syd\|ney`) {
		t.Errorf("pipe not escaped: %q", lines[2])
	}
	if strings.Contains(md, "\t") || strings.Contains(lines[3], "line1\nline2") {
		t.Errorf("newlines/tabs not flattened: %q", lines[3])
	}
	if lines[3] != "| bob | line1 line2 | y z |" {
		t.Errorf("data row = %q", lines[3])
	}
}

func TestCSVMarkdownEmptyHeader(t *testing.T) {
	if md, n := csvMarkdown(nil, nil); md != "" || n != 0 {
		t.Errorf("empty header should produce no output, got %q / %d", md, n)
	}
}

func TestMdCell(t *testing.T) {
	if got := mdCell("  a|b\tc\r\nd  "); got != `a\|b c d` {
		t.Errorf("mdCell = %q", got)
	}
}
