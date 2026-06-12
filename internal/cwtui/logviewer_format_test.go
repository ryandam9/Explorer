package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestPrettifyJSON(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string // substrings expected in the output, in order
		same bool     // output must be the unchanged input
	}{
		{
			name: "whole message is an object",
			in:   `{"user":"bob","ok":true}`,
			want: []string{"{\n", `  "user": "bob"`, `  "ok": true`},
		},
		{
			name: "prefix text then payload",
			in:   `2026-06-12 INFO login {"user":"bob"}`,
			want: []string{"2026-06-12 INFO login\n", `  "user": "bob"`},
		},
		{
			name: "payload then trailing text",
			in:   `{"a":1} took 12ms`,
			want: []string{`  "a": 1`, "\ntook 12ms"},
		},
		{
			name: "array payload",
			in:   `items: [1,2,3]`,
			want: []string{"items:\n", "[\n  1,\n  2,\n  3\n]"},
		},
		{
			name: "nested structures",
			in:   `{"a":{"b":[1]}}`,
			want: []string{"\"a\": {", "\"b\": [", "      1"},
		},
		{name: "plain text untouched", in: "ERROR connection refused", same: true},
		{name: "braces but not JSON", in: "set {x} to {y} in [section]", same: true},
		{name: "bracketed timestamp prefix only", in: "[2026-06-12 01:02:03] hello", same: true},
		{name: "invalid json untouched", in: `{"a":}`, same: true},
		{name: "empty", in: "", same: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prettifyJSON(tt.in)
			if tt.same {
				if got != tt.in {
					t.Fatalf("expected input unchanged, got %q", got)
				}
				return
			}
			rest := got
			for _, w := range tt.want {
				i := strings.Index(rest, w)
				if i < 0 {
					t.Fatalf("output missing %q (in order)\ninput:  %q\noutput: %q", w, tt.in, got)
				}
				rest = rest[i+len(w):]
			}
		})
	}
}

func TestViewerFormatJSONRebuild(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, `request done {"status":500,"path":"/api"}`),
	})

	plainLines := len(v.lines)
	if plainLines != 1 {
		t.Fatalf("expected 1 line without formatting, got %d: %v", plainLines, v.lines)
	}

	v.formatJSON = true
	v.rebuild(v.wrapW)
	if len(v.lines) <= plainLines {
		t.Fatalf("expected the JSON payload expanded onto multiple lines, got %v", v.lines)
	}
	joined := strings.Join(v.lines, "\n")
	if !strings.Contains(joined, `"status": 500`) {
		t.Fatalf("expected pretty-printed JSON, got %v", v.lines)
	}
	// The prefix text stays on the first (timestamped) line.
	if !strings.Contains(v.lines[0], "request done") {
		t.Fatalf("expected prefix kept on the first line, got %q", v.lines[0])
	}

	// Search matches are recomputed against the formatted lines.
	v.term = "status"
	v.computeMatches()
	if len(v.matches) != 1 {
		t.Fatalf("expected 1 match on formatted lines, got %v", v.matches)
	}

	// Toggling back restores the raw single line.
	v.formatJSON = false
	v.rebuild(v.wrapW)
	if len(v.lines) != 1 {
		t.Fatalf("expected the raw line back, got %v", v.lines)
	}
}
