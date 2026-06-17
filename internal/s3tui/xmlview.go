package s3tui

import (
	"bytes"
	"encoding/xml"
	"regexp"
	"strings"
)

// xmlBOM is the UTF-8 byte-order mark that prefixes many Windows/.NET XML
// files; it must be stripped before the content reads as starting with "<".
const xmlBOM = "\ufeff"

// ansiEscape matches the common terminal escape sequences (CSI/SGR colour codes
// and the OSC/two-byte forms) so they can be stripped from previewed text.
var ansiEscape = regexp.MustCompile(`\x1b[@-Z\\-_]|\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// looksLikeXMLContent reports whether content is (or starts as) an XML/HTML
// document, so the preview can pretty-print it. A conservative check: after a
// leading BOM/whitespace it must start with "<" and either be an XML
// declaration or contain a real element.
func looksLikeXMLContent(s string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(s, xmlBOM))
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
	s = strings.TrimPrefix(s, xmlBOM)
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
		switch t := tok.(type) {
		case xml.CharData:
			// Drop layout-only whitespace so the encoder's own indentation
			// controls the result instead of doubling up blank space.
			if strings.TrimSpace(string(t)) == "" {
				continue
			}
		case xml.StartElement:
			// The decoder resolves each element's namespace into Name.Space (the
			// URI). Re-encoding that makes the encoder emit a fresh xmlns on every
			// element — and a duplicate on the declaring element, which already
			// carries an xmlns attribute. Clear the resolved URI so the original
			// xmlns attributes (fixed up below) are the single source of the
			// declaration. Start and end must match, so EndElement is cleared too.
			t.Name.Space = ""
			for i := range t.Attr {
				a := &t.Attr[i]
				switch {
				case a.Name.Space == "xmlns":
					// An "xmlns:prefix" declaration. The encoder's namespace
					// machinery otherwise mangles it into "_xmlns:prefix"; emit it
					// literally instead.
					a.Name = xml.Name{Local: "xmlns:" + a.Name.Local}
				case a.Name.Space != "":
					// A namespaced attribute (e.g. xsi:type): drop the resolved URI
					// so the encoder doesn't synthesise a bogus prefix for it.
					a.Name.Space = ""
				}
			}
			tok = t
		case xml.EndElement:
			t.Name.Space = ""
			tok = t
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
	return declarationOnOwnLine(buf.String()), true
}

// declarationOnOwnLine puts the XML declaration (and any leading processing
// instruction) on its own line — the encoder otherwise runs it straight into
// the root element, e.g. "<?xml ...?><root>".
func declarationOnOwnLine(out string) string {
	if !strings.HasPrefix(out, "<?") {
		return out
	}
	if i := strings.Index(out, "?>"); i >= 0 && i+2 < len(out) && out[i+2] == '<' {
		return out[:i+2] + "\n" + out[i+2:]
	}
	return out
}

// hardWrap breaks each line of s to at most width display columns so a long
// line (e.g. minified XML/JSON, or a wide log line) is fully visible in a
// fixed-width preview pane instead of being silently clipped. width <= 0
// returns s unchanged.
// sanitizeForDisplay makes arbitrary object text safe to render inside the
// preview overlay. Terminal control bytes — carriage returns from progress
// output, ANSI escape sequences, stray C0 controls — otherwise reposition the
// cursor and draw text outside the overlay box, corrupting the layout. The
// classic case is an aws-cli "Completed … KiB\r" progress stream captured in a
// logged stdout.gz: each '\r' snaps the cursor to column 0 of the terminal.
func sanitizeForDisplay(s string) string {
	// Normalize line endings. \r\n → \n; a CR-only file uses \r as its newline.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.Contains(s, "\n") {
		// Progress-style CR overwrites within a line: keep what a terminal would
		// finally show — the text after the last CR on each line — so the progress
		// spam collapses to its final state instead of bleeding out of the box.
		lines := strings.Split(s, "\n")
		for i, ln := range lines {
			if idx := strings.LastIndexByte(ln, '\r'); idx >= 0 {
				lines[i] = ln[idx+1:]
			}
		}
		s = strings.Join(lines, "\n")
	} else {
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return stripControl(s)
}

// stripControl removes ANSI escape sequences and remaining C0/DEL control
// characters (keeping tab and newline) so nothing in the text can move the
// terminal cursor or change its colours while scrolling the preview.
func stripControl(s string) string {
	if i := strings.IndexByte(s, 0x1b); i >= 0 {
		s = ansiEscape.ReplaceAllString(s, "")
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t':
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1 // drop other C0 controls and DEL
		}
		return r
	}, s)
}

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
