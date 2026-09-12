package cwtui

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// splitEventJSON tries to interpret a log message as structured JSON. It
// accepts a leading prefix before the JSON body — the common shape of Lambda
// and container logs ("2026-08-02T10:00:00Z\tINFO\t{...}") — and returns the
// rendered top-level fields, the ordered key list as they appear in the
// document, and any non-JSON remainder (prefix and/or suffix). fields is nil
// when the message contains no parseable JSON object.
func splitEventJSON(msg string) (fields map[string]string, keys []string, raw string) {
	s := strings.TrimSpace(strings.TrimPrefix(msg, "\uFEFF"))
	start := strings.Index(s, "{")
	if start < 0 {
		return nil, nil, s
	}

	dec := json.NewDecoder(strings.NewReader(s[start:]))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, s
	}
	rest, _ := io.ReadAll(dec.Buffered())
	suffix := strings.TrimSpace(string(rest))
	prefix := strings.TrimSpace(s[:start])

	// Key order is lost by map decoding; recover it from the document text so
	// columns follow the order fields were logged in.
	keys = jsonKeyOrder(s[start:], doc)

	fields = make(map[string]string, len(doc))
	for k, v := range doc {
		fields[k] = renderJSONValue(v)
	}

	raw = strings.TrimSpace(prefix + " " + suffix)
	return fields, keys, raw
}

// jsonKeyOrder returns doc's top-level keys in their order of appearance in
// the JSON text. A second decode pass with json.Token keeps this exact (a
// substring search would be fooled by nested keys).
func jsonKeyOrder(src string, doc map[string]any) []string {
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	keys := make([]string, 0, len(doc))
	depth := 0
	expectKey := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				depth++
				expectKey = depth == 1
			case '}':
				depth--
				if depth == 0 {
					return keys
				}
				expectKey = depth == 1
			case '[', ']':
				expectKey = false
			}
		case string:
			if depth == 1 && expectKey {
				keys = append(keys, t)
				expectKey = false
				continue
			}
			if depth == 1 {
				expectKey = true
			}
		default:
			if depth == 1 {
				expectKey = true
			}
		}
	}
	return keys
}

// renderJSONValue renders one JSON value for a table cell. json.Number keeps
// the source representation (no float mangling); nested objects/arrays are
// compacted so the cell hints at the shape without exploding the column.
func renderJSONValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return "?"
		}
		return string(b)
	}
}

// eventTableData is everything the events table needs for one build: the two
// columns, the rows, and the row→event mapping a wrapped message produces.
type eventTableData struct {
	cols   []table.Column
	rows   []table.Row
	groups []int // row → event index; one event can span wrapped rows
}

// buildEventTableData assembles the table content: Time and Message, and
// nothing else. The stream is one keystroke away in the record view (v), so
// the table stays scannable instead of turning into a spreadsheet.
//
// formatJSON ("J") expands JSON embedded in a message into indented lines
// inside the Message column — the same thing J does to the viewer's log lines,
// rather than a different meaning in each view.
//
// avail is the table's usable width: the Message column takes whatever Time
// leaves, so the table fills a wide terminal, and a message too long for that
// width wraps onto extra rows (groups maps them back to their event) rather
// than being cut off.
func buildEventTableData(events []types.FilteredLogEvent, formatJSON bool, avail int) eventTableData {
	d := eventTableData{cols: eventTableColumns()}

	times := make([]string, 0, len(events))
	for _, ev := range events {
		times = append(times, eventTimestamp(ev))
	}
	msgW := messageColumnWidth(avail, []int{fittedWidth("Time", times)})

	d.rows, d.groups = eventTableRows(events, formatJSON, msgW)
	// Pin the Message column to the remaining width: the widget only grows a
	// column to its widest cell, so without this floor a table of short
	// messages would leave the right-hand side of the panel empty.
	d.cols[len(d.cols)-1].Width = msgW
	return d
}
