package s3tui

import (
	"strings"
	"testing"
)

func TestLooksLikeXMLContent(t *testing.T) {
	yes := []string{
		`<?xml version="1.0"?><a/>`,
		"<root><child>x</child></root>",
		"  \n  <note/>",
		"<svg xmlns=\"...\"></svg>",
	}
	for _, s := range yes {
		if !looksLikeXMLContent(s) {
			t.Errorf("should look like XML: %q", s)
		}
	}
	no := []string{
		"", "hello world", `{"json": true}`, "x < y and a > b",
		"plain log line\nsecond line",
	}
	for _, s := range no {
		if looksLikeXMLContent(s) {
			t.Errorf("should NOT look like XML: %q", s)
		}
	}
}

func TestFormatXML(t *testing.T) {
	minified := `<?xml version="1.0"?><config><item id="1">a</item><item id="2">b</item></config>`
	out, ok := formatXML(minified)
	if !ok {
		t.Fatal("expected XML to format")
	}
	// Re-indented onto multiple lines.
	if !strings.Contains(out, "\n  <item") || strings.Count(out, "\n") < 3 {
		t.Errorf("not re-indented:\n%s", out)
	}

	// A truncated document (preview cut mid-tree) yields the part that parsed.
	out, ok = formatXML(`<config><item>a</item><item>unclo`)
	if !ok || !strings.Contains(out, "<item>") {
		t.Errorf("truncated XML should yield a partial render: ok=%v\n%s", ok, out)
	}
}

func TestHardWrap(t *testing.T) {
	// A line longer than width is split; short lines and newlines are preserved.
	got := hardWrap("abcdefghij\nshort", 5)
	want := "abcde\nfghij\nshort"
	if got != want {
		t.Errorf("hardWrap = %q, want %q", got, want)
	}
	if hardWrap("anything", 0) != "anything" {
		t.Error("width 0 should be a no-op")
	}
	// No line exceeds the width after wrapping.
	long := strings.Repeat("x", 250)
	for _, ln := range strings.Split(hardWrap(long, 40), "\n") {
		if len([]rune(ln)) > 40 {
			t.Errorf("wrapped line too long: %d", len([]rune(ln)))
		}
	}
}

func TestXMLBOMHandling(t *testing.T) {
	// A UTF-8 BOM (common in Windows/.NET XML) must not stop detection.
	bom := xmlBOM + `<?xml version="1.0"?><root><a>1</a></root>`
	if !looksLikeXMLContent(bom) {
		t.Fatal("BOM-prefixed XML should be detected as XML")
	}
	out, ok := formatXML(bom)
	if !ok {
		t.Fatal("BOM-prefixed XML should format")
	}
	if strings.Contains(out, xmlBOM) {
		t.Errorf("BOM should be stripped from the formatted output")
	}
	if !strings.Contains(out, "\n  <a>1</a>") {
		t.Errorf("BOM XML should be re-indented:\n%s", out)
	}
}

func TestXMLDeclarationOnOwnLine(t *testing.T) {
	out, ok := formatXML(`<?xml version="1.0"?><root><a>1</a></root>`)
	if !ok {
		t.Fatal("formatXML")
	}
	if !strings.HasPrefix(out, "<?xml version=\"1.0\"?>\n<root>") {
		t.Errorf("declaration should be on its own line:\n%s", out)
	}
}

func TestSanitizeForDisplayCollapsesCarriageReturns(t *testing.T) {
	// aws-cli style progress: repeated CR overwrites on one logical line, then a
	// real newline. The preview should keep only the final segment per line — and
	// crucially leave no CR for the terminal to act on.
	in := "Completed 54.0 KiB/138.0 MiB\rCompleted 56.1 KiB/138.0 MiB\rupload: ./a to s3://b\nnext line"
	got := sanitizeForDisplay(in)
	want := "upload: ./a to s3://b\nnext line"
	if got != want {
		t.Errorf("progress collapse = %q, want %q", got, want)
	}
	if strings.ContainsRune(got, '\r') {
		t.Errorf("sanitized text still contains a carriage return: %q", got)
	}
}

func TestSanitizeForDisplayLineEndings(t *testing.T) {
	if got := sanitizeForDisplay("a\r\nb\r\nc"); got != "a\nb\nc" {
		t.Errorf("CRLF normalize = %q, want a\\nb\\nc", got)
	}
	// CR-only (classic Mac) line endings: CR is the newline, so nothing is lost.
	if got := sanitizeForDisplay("a\rb\rc"); got != "a\nb\nc" {
		t.Errorf("CR-only normalize = %q, want a\\nb\\nc", got)
	}
}

func TestStripControlKeepsTextDropsControls(t *testing.T) {
	// ANSI colour codes and a stray bell/escape are removed; tab and newline stay.
	in := "\x1b[31mred\x1b[0m\ttabbed\x07\nplain"
	got := stripControl(in)
	want := "red\ttabbed\nplain"
	if got != want {
		t.Errorf("stripControl = %q, want %q", got, want)
	}
}

func TestSanitizeThenHardWrapHasNoControlBleed(t *testing.T) {
	// Defence in depth: after sanitizing, hard-wrapping must never emit a CR or
	// ESC that could move the cursor out of the overlay box.
	raw := "INFO\x1b[32m ok\x1b[0m progress\rdone with a very long tail that exceeds the wrap width easily\nsecond"
	out := hardWrap(sanitizeForDisplay(raw), 20)
	for _, r := range out {
		if r == '\r' || r == 0x1b {
			t.Fatalf("wrapped preview contains a control byte %#x: %q", r, out)
		}
	}
}
