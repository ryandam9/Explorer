package s3tui

import (
	"bytes"
	"encoding/json"
	"strings"
)

// formatJSONContent re-indents a JSON document for readable display, the JSON
// counterpart of formatXML: a minified API dump or event payload becomes an
// indented, scrollable block instead of one giant hard-wrapped line (#225).
// Detection is by content, not filename, so it also catches JSON stored under
// extension-less keys, ".txt", or the inner document of a ".json.gz" (whose
// preview key keeps the .gz name). The check is conservative: after a leading
// BOM/whitespace the text must be a single valid JSON object or array — the
// truncated tail of a capped preview, NDJSON, or brace-leading non-JSON all
// fail to parse and fall back to the raw text (ok=false), so nothing is ever
// mangled by a wrong guess.
func formatJSONContent(s string) (string, bool) {
	t := strings.TrimSpace(strings.TrimPrefix(s, xmlBOM))
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return "", false
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(t), "", "  "); err != nil {
		return "", false
	}
	return buf.String(), true
}
