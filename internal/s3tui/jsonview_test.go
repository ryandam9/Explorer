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

	// The truncated tail of a capped preview, NDJSON, and non-JSON text must
	// all fall back to the raw view rather than be mangled.
	for _, bad := range []string{
		`{"cut":"mid-doc`,      // truncated
		"{\"a\":1}\n{\"b\":2}", // NDJSON: two documents
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
