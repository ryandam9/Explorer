package cwtui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// The viewer holds the whole query window up to viewerMaxEvents; crossing that
// ceiling drops the oldest events and must flag the log as truncated so the
// header and status bar never present a capped log as the complete one.
func TestViewerAppendFlagsTruncationAtCeiling(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}

	// Below the ceiling: everything is kept, nothing is flagged.
	batch := make([]types.FilteredLogEvent, 0, viewerMaxEvents)
	for i := 0; i < viewerMaxEvents; i++ {
		batch = append(batch, testEvent(fmt.Sprintf("e%d", i), int64(i+1), "msg"))
	}
	v.append(batch)
	if len(v.events) != viewerMaxEvents {
		t.Fatalf("events = %d, want %d", len(v.events), viewerMaxEvents)
	}
	if v.truncated {
		t.Errorf("truncated set at exactly the ceiling")
	}
	if v.truncNote() != "" {
		t.Errorf("truncNote = %q, want empty", v.truncNote())
	}

	// One more event pushes past it: the oldest is dropped and said so.
	v.append([]types.FilteredLogEvent{testEvent("overflow", int64(viewerMaxEvents+1), "msg")})
	if len(v.events) != viewerMaxEvents {
		t.Fatalf("events after overflow = %d, want %d", len(v.events), viewerMaxEvents)
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
}

// Re-opening the viewer on another stream starts a fresh log: a truncation
// flagged for the previous target must not carry over.
func TestViewerOpenResetsTruncation(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}
	v.truncated = true

	v.open(viewerKey{region: "us-east-1", group: "/g", stream: "s"}, "s [us-east-1]", 80)
	if v.truncated {
		t.Errorf("truncated survived open()")
	}
	if v.truncNote() != "" {
		t.Errorf("truncNote = %q after open(), want empty", v.truncNote())
	}
}
