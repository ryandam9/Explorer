package s3tui

import (
	"bytes"
	"encoding/xml"
	"strings"
)

// looksLikeXMLContent reports whether content is (or starts as) an XML/HTML
// document, so the preview can pretty-print it. A conservative check: it must
// start with "<" and either be an XML declaration or contain a real element.
func looksLikeXMLContent(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" || t[0] != '<' {
		return false
	}
	if strings.HasPrefix(strings.ToLower(t), "<?xml") {
		return true
	}
	return strings.Contains(t, "</") || strings.Contains(t, "/>")
}

// formatXML re-indents an XML document for readable display. It is tolerant of
// the truncated tail of a preview (and of mildly malformed input): tokens are
// re-emitted with indentation until the first decode error, then flushed. ok is
// false when nothing parsed, so the caller can fall back to the raw text.
func formatXML(s string) (string, bool) {
	dec := xml.NewDecoder(strings.NewReader(s))
	dec.Strict = false

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")

	any := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break // EOF, or the truncated/malformed tail of a preview
		}
		// Drop layout-only whitespace so the encoder's own indentation controls
		// the result instead of doubling up blank space.
		if cd, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(cd)) == "" {
			continue
		}
		if err := enc.EncodeToken(tok); err != nil {
			break
		}
		any = true
	}
	if !any {
		return "", false
	}
	_ = enc.Flush()
	return buf.String(), true
}

// hardWrap breaks each line of s to at most width display columns so a long
// line (e.g. minified XML/JSON, or a wide log line) is fully visible in a
// fixed-width preview pane instead of being silently clipped. width <= 0
// returns s unchanged.
func hardWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var b strings.Builder
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		for {
			r := []rune(line)
			if len(r) <= width {
				b.WriteString(line)
				break
			}
			b.WriteString(string(r[:width]))
			b.WriteByte('\n')
			line = string(r[width:])
		}
	}
	return b.String()
}
