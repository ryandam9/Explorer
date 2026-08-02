package cwtui

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/ryandam9/aws_explorer/internal/table"
)

// maxFieldCols caps how many JSON field columns the events table shows; the
// overflow count is surfaced next to the table so a cap never reads as "these
// are all the fields" (the raw message still holds everything).
const maxFieldCols = 24

// maxFieldCell caps one JSON field value inside a cell. Field columns are
// meant to be scannable; a huge value would turn its column into the old
// single-message problem. The full value is always in the raw event (Enter/y).
const maxFieldCell = 80

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

// eventView is one event analyzed for the table: pre-rendered JSON fields
// (nil for plain-text events) plus whatever text was not part of the JSON.
type eventView struct {
	fields map[string]string
	raw    string
}

// analyzeEvents parses every message once and derives the union of JSON field
// keys in first-appearance order. anyJSON reports whether field columns are
// worth showing at all; anyRaw whether a Message column is still needed for
// plain-text events, prefixes and suffixes.
func analyzeEvents(events []types.FilteredLogEvent) (views []eventView, keys []string, anyJSON, anyRaw bool) {
	views = make([]eventView, 0, len(events))
	seen := make(map[string]bool)
	for _, ev := range events {
		fields, evKeys, raw := splitEventJSON(aws.ToString(ev.Message))
		if fields != nil {
			anyJSON = true
			for _, k := range evKeys {
				if !seen[k] {
					seen[k] = true
					keys = append(keys, k)
				}
			}
		}
		if raw != "" {
			anyRaw = true
		}
		views = append(views, eventView{fields: fields, raw: raw})
	}
	return views, keys, anyJSON, anyRaw
}

// clipFieldCell truncates a JSON field value for its column.
func clipFieldCell(s string) string {
	s = flattenEventText(s)
	r := []rune(s)
	if len(r) <= maxFieldCell {
		return s
	}
	return string(r[:maxFieldCell-1]) + "…"
}

// eventTableData is everything the events table needs for one build: columns,
// rows, the pan state (shift comes back clamped) and the field-cap overflow.
type eventTableData struct {
	cols         []table.Column
	rows         []table.Row
	split        bool // JSON field columns in use
	hiddenFields int  // field keys beyond maxFieldCols
	maxMsgLen    int  // pan bound for the message/raw column
	shift        int  // msgShift after clamping against maxMsgLen
}

// buildEventTableData assembles the table content. With split enabled and at
// least one JSON event, each top-level JSON field becomes its own column
// (capped at maxFieldCols, first-appearance order) and the Message column
// carries only what wasn't JSON; otherwise the plain Time/[Stream]/Message
// layout is used. msgShift pans the Message column in both layouts.
func buildEventTableData(events []types.FilteredLogEvent, withStream, split bool, msgShift int) eventTableData {
	views, keys, anyJSON, anyRaw := analyzeEvents(events)

	if !split || !anyJSON {
		d := eventTableData{
			cols:      eventTableColumns(withStream),
			maxMsgLen: maxEventMsgLen(events),
		}
		d.shift = clampMsgShift(msgShift, d.maxMsgLen)
		d.rows = eventTableRows(events, withStream, d.shift)
		return d
	}

	d := eventTableData{split: true}
	if len(keys) > maxFieldCols {
		d.hiddenFields = len(keys) - maxFieldCols
		keys = keys[:maxFieldCols]
	}
	for _, v := range views {
		if n := len([]rune(flattenEventText(v.raw))); n > d.maxMsgLen {
			d.maxMsgLen = n
		}
	}
	d.shift = clampMsgShift(msgShift, d.maxMsgLen)

	// Fixed columns are excluded from the "(1) (2) …" numbering so the field
	// numbers survive column-scrolling landmarks.
	d.cols = []table.Column{{Title: "Time", Width: 4, NoNumber: true}}
	if withStream {
		d.cols = append(d.cols, table.Column{Title: "Stream", Width: 4, NoNumber: true})
	}
	for _, k := range keys {
		d.cols = append(d.cols, table.Column{Title: clipFieldCell(k), Width: 4})
	}
	if anyRaw {
		d.cols = append(d.cols, table.Column{Title: "Message", Width: 4, NoNumber: true})
	}

	d.rows = make([]table.Row, 0, len(events))
	for i, ev := range events {
		t := eventTimestamp(ev)
		row := table.Row{t}
		if withStream {
			row = append(row, clipEventCell(aws.ToString(ev.LogStreamName)))
		}
		for _, k := range keys {
			row = append(row, clipFieldCell(views[i].fields[k])) // "" when absent
		}
		if anyRaw {
			row = append(row, eventMessageCell(views[i].raw, d.shift))
		}
		d.rows = append(d.rows, row)
	}
	return d
}
