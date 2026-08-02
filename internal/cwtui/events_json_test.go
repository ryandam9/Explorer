package cwtui

import (
	"fmt"
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

func TestBuildEventTableDataSplitsJSON(t *testing.T) {
	events := []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1700000000000), Message: aws.String(`{"level":"info","msg":"started"}`)},
		{Timestamp: aws.Int64(1700000001000), Message: aws.String(`{"level":"error","msg":"boom","requestId":"r-1"}`)},
		{Timestamp: aws.Int64(1700000002000), Message: aws.String("START RequestId: r-1")}, // plain text
	}

	d := buildEventTableData(events, false, true, 0)
	if !d.split {
		t.Fatal("split should engage when JSON events are present")
	}
	titles := make([]string, len(d.cols))
	for i, c := range d.cols {
		titles[i] = c.Title
	}
	want := "Time,level,msg,requestId,Message"
	if got := strings.Join(titles, ","); got != want {
		t.Fatalf("columns = %s, want %s", got, want)
	}

	// JSON rows: fields filled, absent field blank, Message column empty.
	if d.rows[0][1] != "info" || d.rows[0][2] != "started" {
		t.Errorf("row 0 fields = %v", d.rows[0])
	}
	if d.rows[0][3] != "" {
		t.Errorf("absent requestId should be blank, got %q", d.rows[0][3])
	}
	if d.rows[0][4] != "" {
		t.Errorf("fully-JSON event should leave Message blank, got %q", d.rows[0][4])
	}
	// Plain-text row: fields blank, message in the raw column.
	if d.rows[2][1] != "" || d.rows[2][4] != "START RequestId: r-1" {
		t.Errorf("plain row = %v", d.rows[2])
	}

	// The J toggle (split=false) falls back to the plain layout.
	d = buildEventTableData(events, false, false, 0)
	if d.split || len(d.cols) != 2 {
		t.Errorf("split off: cols = %v", d.cols)
	}
}

func TestBuildEventTableDataFieldCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < maxFieldCols+5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `"k%02d":%d`, i, i)
	}
	b.WriteString("}")
	events := []types.FilteredLogEvent{{Timestamp: aws.Int64(1700000000000), Message: aws.String(b.String())}}

	d := buildEventTableData(events, false, true, 0)
	if d.hiddenFields != 5 {
		t.Errorf("hiddenFields = %d, want 5", d.hiddenFields)
	}
	// Time + capped fields (no Message column: nothing raw remained).
	if len(d.cols) != 1+maxFieldCols {
		t.Errorf("cols = %d, want %d", len(d.cols), 1+maxFieldCols)
	}
}

func TestBuildEventTableDataNoJSONFallsBack(t *testing.T) {
	events := []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1700000000000), Message: aws.String("plain one")},
		{Timestamp: aws.Int64(1700000001000), Message: aws.String("plain two")},
	}
	d := buildEventTableData(events, false, true, 0)
	if d.split {
		t.Error("split must not engage without any JSON event")
	}
	if len(d.cols) != 2 || d.cols[1].Title != "Message" {
		t.Errorf("fallback cols = %v", d.cols)
	}
}
