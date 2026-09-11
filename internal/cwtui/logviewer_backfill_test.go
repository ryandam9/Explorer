package cwtui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// The viewer's ceiling comes from --max-events, then cw.maxEvents, then the
// built-in default; a negative value at either level means "no ceiling".
func TestResolveMaxEvents(t *testing.T) {
	tests := []struct {
		name       string
		flag, conf int
		want       int
	}{
		{"neither set falls back to the default", 0, 0, defaultMaxEvents},
		{"config used when the flag is unset", 0, 1234, 1234},
		{"flag wins over config", 500, 1234, 500},
		{"negative flag means unlimited", -1, 1234, 0},
		{"negative config means unlimited", 0, -1, 0},
		{"flag overrides an unlimited config", 500, -1, 500},
		{"negative flag overrides a config ceiling", -7, 100, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveMaxEvents(tt.flag, tt.conf); got != tt.want {
				t.Errorf("ResolveMaxEvents(%d, %d) = %d, want %d", tt.flag, tt.conf, got, tt.want)
			}
		})
	}
}

// The viewer holds the whole query window up to its ceiling; crossing it drops
// the oldest events and must flag the log as truncated so the header and status
// bar never present a capped log as the complete one.
func TestViewerAppendFlagsTruncationAtCeiling(t *testing.T) {
	const ceiling = 500
	v := &logViewer{seen: map[string]bool{}, wrapW: 80, maxEvents: ceiling}

	// Below the ceiling: everything is kept, nothing is flagged.
	batch := make([]types.FilteredLogEvent, 0, ceiling)
	for i := 0; i < ceiling; i++ {
		batch = append(batch, testEvent(fmt.Sprintf("e%d", i), int64(i+1), "msg"))
	}
	v.append(batch)
	if len(v.events) != ceiling {
		t.Fatalf("events = %d, want %d", len(v.events), ceiling)
	}
	if v.truncated {
		t.Errorf("truncated set at exactly the ceiling")
	}
	if v.truncNote() != "" {
		t.Errorf("truncNote = %q, want empty", v.truncNote())
	}

	// One more event pushes past it: the oldest is dropped and said so.
	v.append([]types.FilteredLogEvent{testEvent("overflow", int64(ceiling+1), "msg")})
	if len(v.events) != ceiling {
		t.Fatalf("events after overflow = %d, want %d", len(v.events), ceiling)
	}
	if aws.ToString(v.events[0].EventId) != "e1" {
		t.Errorf("oldest retained event = %q, want e1", aws.ToString(v.events[0].EventId))
	}
	if !v.truncated {
		t.Errorf("truncated not set after dropping the oldest event")
	}
	if !strings.Contains(v.truncNote(), "truncated") {
		t.Errorf("truncNote = %q, want a truncation note", v.truncNote())
	}
	if !strings.Contains(v.truncNote(), fmt.Sprint(ceiling)) {
		t.Errorf("truncNote = %q, want it to name the ceiling %d", v.truncNote(), ceiling)
	}
}

// maxEvents 0 is "no ceiling": the viewer keeps everything it is given and
// never claims truncation on its own.
func TestViewerAppendUnlimitedKeepsEverything(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80, maxEvents: 0}

	const n = defaultMaxEvents + 10
	batch := make([]types.FilteredLogEvent, 0, n)
	for i := 0; i < n; i++ {
		batch = append(batch, testEvent(fmt.Sprintf("e%d", i), int64(i+1), "msg"))
	}
	v.append(batch)

	if len(v.events) != n {
		t.Fatalf("events = %d, want %d (no ceiling)", len(v.events), n)
	}
	if v.truncated {
		t.Errorf("truncated set with no ceiling configured")
	}
}

// Re-opening the viewer on another stream starts a fresh log: a truncation
// flagged for the previous target must not carry over, and the ceiling the
// model resolved is applied to the new one.
func TestViewerOpenResetsTruncation(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}
	v.truncated = true

	v.open(viewerKey{region: "us-east-1", group: "/g", stream: "s"}, "s [us-east-1]", 80, 2500)
	if v.truncated {
		t.Errorf("truncated survived open()")
	}
	if v.maxEvents != 2500 {
		t.Errorf("maxEvents = %d, want 2500", v.maxEvents)
	}
	if v.truncNote() != "" {
		t.Errorf("truncNote = %q after open(), want empty", v.truncNote())
	}
}
