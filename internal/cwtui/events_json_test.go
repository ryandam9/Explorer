package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestSplitEventJSON(t *testing.T) {
	// A plain JSON object: every field captured, keys in document order.
	fields, keys, raw := splitEventJSON(`{"level":"error","msg":"boom","count":3,"ratio":1.50,"ok":false,"ctx":null}`)
	if fields == nil {
		t.Fatal("JSON object not recognized")
	}
	wantKeys := []string{"level", "msg", "count", "ratio", "ok", "ctx"}
	if strings.Join(keys, ",") != strings.Join(wantKeys, ",") {
		t.Errorf("keys = %v, want %v (document order)", keys, wantKeys)
	}
	if raw != "" {
		t.Errorf("raw = %q, want empty for a fully-JSON message", raw)
	}
	for k, want := range map[string]string{
		"level": "error",
		"count": "3",
		"ratio": "1.50", // json.Number keeps the source formatting
		"ok":    "false",
		"ctx":   "null", // null is a value, distinct from an absent field
	} {
		if fields[k] != want {
			t.Errorf("fields[%q] = %q, want %q", k, fields[k], want)
		}
	}

	// Lambda-style: timestamp/request-id/level prefix before the JSON body.
	fields, _, raw = splitEventJSON("2026-08-02T10:00:00.000Z\tabc-123\tINFO\t" + `{"msg":"hi"}`)
	if fields == nil || fields["msg"] != "hi" {
		t.Fatalf("prefixed JSON not recognized: %v", fields)
	}
	if !strings.Contains(raw, "INFO") {
		t.Errorf("prefix should be kept as raw remainder, got %q", raw)
	}

	// Nested structures render compact; key order ignores nested keys.
	fields, keys, _ = splitEventJSON(`{"a":{"z":1,"y":2},"b":[1,2]}`)
	if fields["a"] != `{"y":2,"z":1}` && fields["a"] != `{"z":1,"y":2}` {
		t.Errorf("nested object cell = %q", fields["a"])
	}
	if fields["b"] != "[1,2]" {
		t.Errorf("array cell = %q", fields["b"])
	}
	if strings.Join(keys, ",") != "a,b" {
		t.Errorf("nested keys leaked into column order: %v", keys)
	}

	// Non-JSON stays raw; a brace mid-text that isn't JSON must not parse.
	if fields, _, raw = splitEventJSON("plain text line"); fields != nil || raw != "plain text line" {
		t.Errorf("plain text mis-parsed: fields=%v raw=%q", fields, raw)
	}
	if fields, _, _ = splitEventJSON("weird {not json}"); fields != nil {
		t.Errorf("invalid brace content mis-parsed: %v", fields)
	}
}

// The table is Time and Message, whatever the message contains: JSON is not
// broken out into columns any more (the record view shows the fields).
func TestBuildEventTableDataIsTimeAndMessageOnly(t *testing.T) {
	events := []types.FilteredLogEvent{
		{
			Timestamp:     aws.Int64(1),
			LogStreamName: aws.String("stream-a"),
			Message:       aws.String(`{"level":"error","msg":"boom"}`),
		},
		{
			Timestamp:     aws.Int64(2),
			LogStreamName: aws.String("stream-b"),
			Message:       aws.String("plain text"),
		},
	}

	d := buildEventTableData(events, 200)

	if len(d.cols) != 2 {
		t.Fatalf("columns = %d, want 2", len(d.cols))
	}
	if d.cols[0].Title != "Time" || d.cols[1].Title != "Message" {
		t.Errorf("columns = %q/%q, want Time/Message", d.cols[0].Title, d.cols[1].Title)
	}

	// Even for a whole-group view the stream is absent from the table; it is
	// in the record view instead.
	for _, c := range d.cols {
		if c.Title == "Stream" {
			t.Errorf("stream column present, want it dropped")
		}
	}

	// The JSON message stays intact in the Message cell rather than being
	// spread across field columns.
	if got := d.rows[0][1]; !strings.Contains(got, `"level"`) || !strings.Contains(got, "boom") {
		t.Errorf("json message cell = %q, want the raw message", got)
	}
	if len(d.rows) != len(d.groups) {
		t.Errorf("rows = %d, groups = %d", len(d.rows), len(d.groups))
	}
}
