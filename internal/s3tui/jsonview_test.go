package s3tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"
)

func TestFormatJSONContent(t *testing.T) {
	minified := `{"level":"info","items":[1,2,3],"nested":{"ok":true}}`
	out, ok := formatJSONContent(minified)
	if !ok {
		t.Fatal("minified JSON object not detected")
	}
	if lines := strings.Split(out, "\n"); len(lines) < 5 {
		t.Errorf("expected an indented multi-line document, got %d lines: %q", len(lines), out)
	}
	if !strings.Contains(out, "  \"level\": \"info\"") {
		t.Errorf("missing indented field, got %q", out)
	}

	// Arrays and BOM-prefixed documents are detected too.
	if _, ok := formatJSONContent(`[{"a":1},{"b":2}]`); !ok {
		t.Error("JSON array not detected")
	}
	if _, ok := formatJSONContent("\ufeff  {\"a\":1}"); !ok {
		t.Error("BOM-prefixed JSON not detected")
	}

	// Non-JSON and a single document cut mid-way fall back to the raw view.
	for _, bad := range []string{
		`{"cut":"mid-doc`, // truncated single document
		"plain text",
		"42",
		"{not json}",
		"",
	} {
		if _, ok := formatJSONContent(bad); ok {
			t.Errorf("false positive for %q", bad)
		}
	}
}

// S3 objects are often a stream of documents: NDJSON (one per line) and
// Firehose-style concatenated objects both format, every document indented.
func TestFormatJSONContentDocumentStreams(t *testing.T) {
	for name, in := range map[string]string{
		"ndjson":       "{\"a\":1}\n{\"b\":2}\n{\"c\":3}",
		"concatenated": `{"a":1}{"b":2}{"c":3}`,
	} {
		out, ok := formatJSONContent(in)
		if !ok {
			t.Errorf("%s: not formatted", name)
			continue
		}
		for _, want := range []string{"  \"a\": 1", "  \"b\": 2", "  \"c\": 3"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing indented %q in %q", name, want, out)
			}
		}
	}
}

// The unparsed remainder — a capped preview's truncated tail, or trailing
// non-JSON — is appended verbatim after the formatted documents, so nothing
// is ever lost.
func TestFormatJSONContentKeepsUnparsedTail(t *testing.T) {
	out, ok := formatJSONContent("{\"a\":1}\n{\"cut\":\"mid")
	if !ok {
		t.Fatal("leading complete document should still format")
	}
	if !strings.Contains(out, "  \"a\": 1") {
		t.Errorf("first document not indented: %q", out)
	}
	if !strings.Contains(out, `{"cut":"mid`) {
		t.Errorf("truncated tail lost: %q", out)
	}

	out, ok = formatJSONContent(`{"a":1} trailing note`)
	if !ok || !strings.Contains(out, "trailing note") {
		t.Errorf("trailing text lost: ok=%v %q", ok, out)
	}
}

// A minified JSON object previews as an indented document even when the key
// doesn't say .json (content detection), and the search/grep machinery sees
// the formatted lines.
func TestPreviewPrettyPrintsJSONByContent(t *testing.T) {
	for _, key := range []string{"data.json", "payload.txt", "export.json.gz"} {
		m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
			showPreview: true, previewKey: key,
			previewContent: `{"alpha":1,"beta":{"deep":"value"}}`}
		m.previewSearchInput = textinput.New()
		m.previewGrepInput = textinput.New()
		m.initPreviewViewport(m.previewContent, nil)

		if len(m.previewPlain) < 5 {
			t.Errorf("%s: expected indented multi-line JSON, got %d lines: %q", key, len(m.previewPlain), m.previewPlain)
			continue
		}
		if got := ansi.Strip(strings.TrimSpace(m.previewPlain[1])); got != `"alpha": 1,` {
			t.Errorf("%s: line 2 = %q, want %q", key, got, `"alpha": 1,`)
		}
	}
}

// An NDJSON object streams through the full preview pipeline formatted.
func TestPreviewPrettyPrintsNDJSON(t *testing.T) {
	m := &Model{width: 100, height: 30, state: stateObjectList, focus: focusObjects,
		showPreview: true, previewKey: "events.json",
		previewContent: "{\"id\":1,\"ok\":true}\n{\"id\":2,\"ok\":false}"}
	m.previewSearchInput = textinput.New()
	m.previewGrepInput = textinput.New()
	m.initPreviewViewport(m.previewContent, nil)

	if len(m.previewPlain) < 8 {
		t.Fatalf("expected both documents indented, got %d lines: %q", len(m.previewPlain), m.previewPlain)
	}
	joined := strings.Join(m.previewPlain, "\n")
	for _, want := range []string{"  \"id\": 1,", "  \"id\": 2,"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing indented %q in preview lines", want)
		}
	}
}
