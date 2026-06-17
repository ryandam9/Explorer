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

// A namespaced document (e.g. ISO 20022 payment XML) must not have its single
// xmlns declaration duplicated onto every element by the encoder round-trip.
func TestFormatXMLNamespaceNotDuplicated(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8"?>` +
		`<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pain.002.001.03">` +
		`<CstmrPmtStsRpt><GrpHdr><MsgId>ABC</MsgId></GrpHdr>` +
		`<RmtInf><Ustrd>remittance</Ustrd></RmtInf></CstmrPmtStsRpt></Document>`
	out, ok := formatXML(doc)
	if !ok {
		t.Fatal("namespaced XML should format")
	}
	if n := strings.Count(out, "xmlns="); n != 1 {
		t.Errorf("xmlns should appear once, got %d:\n%s", n, out)
	}
	// The single declaration stays on the root element.
	if !strings.Contains(out, `<Document xmlns="urn:iso:std:iso:20022:tech:xsd:pain.002.001.03">`) {
		t.Errorf("root declaration missing/changed:\n%s", out)
	}
	if !strings.Contains(out, "<Ustrd>remittance</Ustrd>") {
		t.Errorf("inner element should render without an xmlns:\n%s", out)
	}
}

// Prefixed namespace declarations must survive the round-trip cleanly rather
// than being mangled into the encoder's "_xmlns" artefact.
func TestFormatXMLPrefixedNamespaceNotMangled(t *testing.T) {
	out, ok := formatXML(`<ns:Doc xmlns:ns="urn:x"><ns:A>v</ns:A></ns:Doc>`)
	if !ok {
		t.Fatal("prefixed XML should format")
	}
	if strings.Contains(out, "_xmlns") {
		t.Errorf("xmlns:prefix declaration was mangled:\n%s", out)
	}
	if !strings.Contains(out, `xmlns:ns="urn:x"`) {
		t.Errorf("prefixed declaration missing:\n%s", out)
	}
}

// When an XML preview is truncated mid-element, the "preview truncated" note
// must be its own trailing line — not swallowed into the last open element's
// text (the original bug: the note rendered as the <Ustrd> element's body).
func TestBuildPreviewDisplayTruncationNoteNotAbsorbed(t *testing.T) {
	// A document cut mid-element: <Ustrd> is never closed.
	content := `<?xml version="1.0"?><Document xmlns="urn:x"><RmtInf><Ustrd>(h1)remediation pl`
	out := buildPreviewDisplay(content, true, 200)

	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "… preview truncated …") {
		t.Errorf("truncation note should be the trailing line:\n%s", out)
	}
	// The note must stand alone, not glued to the element text.
	if strings.Contains(out, "remediation pl… preview truncated …") ||
		strings.Contains(out, "remediation pl …") {
		t.Errorf("note was absorbed into element text:\n%s", out)
	}
	// It still pretty-prints the part that parsed.
	if !strings.Contains(out, "<RmtInf>") {
		t.Errorf("parsed prefix should still render:\n%s", out)
	}
}

func TestBuildPreviewDisplayNoNoteWhenComplete(t *testing.T) {
	out := buildPreviewDisplay("<root><a>1</a></root>", false, 200)
	if strings.Contains(out, "truncated") {
		t.Errorf("complete preview should carry no truncation note:\n%s", out)
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
