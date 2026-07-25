package s3tui

import (
	"bytes"
	"encoding/json"
	"strings"
)

// formatJSONContent re-indents JSON for readable display, the JSON counterpart
// of formatXML: a minified API dump or event payload becomes an indented,
// scrollable block instead of one giant hard-wrapped line (#225). Detection is
// by content, not filename, so it also catches JSON stored under
// extension-less keys, ".txt", or the inner document of a ".json.gz" (whose
// preview key keeps the .gz name).
//
// S3 objects are very often a *stream* of documents rather than a single one —
// NDJSON / JSON Lines, or Firehose-style concatenated objects with no
// separator at all — so documents are decoded and indented one after another
// until the text runs out. Anything left unparsed (the truncated tail of a
// capped preview, or trailing non-JSON) is appended verbatim, so content is
// never lost or mangled. ok is false when not even the first document parses —
// brace-leading non-JSON, or a single document cut mid-way — and the caller
// falls back to the raw text.
func formatJSONContent(s string) (string, bool) {
	t := strings.TrimSpace(strings.TrimPrefix(s, xmlBOM))
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	var b strings.Builder
	docs := 0
	tailFrom := 0
	for {
		start := int(dec.InputOffset())
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			tailFrom = start
			break
		}
		var buf bytes.Buffer
		if json.Indent(&buf, raw, "", "  ") != nil {
			tailFrom = start
			break
		}
		if docs > 0 {
			b.WriteByte('\n')
		}
		b.Write(buf.Bytes())
		docs++
		tailFrom = int(dec.InputOffset())
	}
	if docs == 0 {
		return "", false
	}
	if tail := strings.TrimSpace(t[tailFrom:]); tail != "" {
		b.WriteByte('\n')
		b.WriteString(tail)
	}
	return b.String(), true
}
