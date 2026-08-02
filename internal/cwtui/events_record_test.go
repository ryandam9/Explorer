package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// The record view must show every field's complete value — the whole point is
// escaping the table's cell clipping.
func TestEventRecordEntriesFullValues(t *testing.T) {
	long := strings.Repeat("arn:aws:thing/", 30) // ~420 runes, well past maxFieldCell
	ev := types.FilteredLogEvent{
		Timestamp:     aws.Int64(1700000000000),
		LogStreamName: aws.String("stream-a"),
		Message:       aws.String(`{"level":"info","resource":"` + long + `"}`),
	}

	entries := eventRecordEntries(ev)
	labels := make([]string, len(entries))
	byLabel := map[string]string{}
	for i, e := range entries {
		labels[i] = e.label
		byLabel[e.label] = e.value
	}
	if strings.Join(labels, ",") != "Time,Stream,level,resource" {
		t.Fatalf("labels = %v", labels)
	}
	if byLabel["resource"] != long {
		t.Errorf("resource value clipped: %d runes, want %d", len(byLabel["resource"]), len(long))
	}

	// Plain-text event: Time + Message with the full text.
	plain := eventRecordEntries(types.FilteredLogEvent{
		Timestamp: aws.Int64(1700000000000),
		Message:   aws.String("START RequestId: r-1"),
	})
	if len(plain) != 2 || plain[1].label != "Message" || plain[1].value != "START RequestId: r-1" {
		t.Errorf("plain entries = %v", plain)
	}
}

// Long values wrap across lines in the viewport content, with nothing lost.
func TestEventRecordLinesWrapWithoutLoss(t *testing.T) {
	long := strings.Repeat("x", 200)
	entries := []eventRecordEntry{{"resource", long}}
	lines := eventRecordLines(entries, 60)
	if len(lines) < 4 {
		t.Fatalf("expected the 200-rune value to wrap over several 60-wide lines, got %d", len(lines))
	}
	joined := strings.ReplaceAll(strings.Join(lines, ""), " ", "")
	if !strings.Contains(joined, long) {
		t.Error("wrapped lines lost part of the value")
	}
	for i, ln := range lines {
		if n := len([]rune(ln)); n > 60 {
			t.Errorf("line %d is %d runes wide, want <= 60", i, n)
		}
	}
}

// v opens the record for the selected event; its own keys scroll/close it and
// stray keys are ignored rather than reaching the browser.
func TestEventRecordOpenClose(t *testing.T) {
	m := &model{width: 100, height: 30}
	m.view = viewEvents
	m.focus = focusEvents
	m.events = []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1700000000000), Message: aws.String(`{"resource":"` + strings.Repeat("r", 300) + `"}`)},
		{Timestamp: aws.Int64(1700000001000), Message: aws.String("second")},
	}
	m.selectedEventIdx = 0

	newModel, _ := m.Update(keyMsg("v"))
	m2 := newModel.(*model)
	if !m2.recordActive {
		t.Fatal("v should open the record view")
	}
	if !strings.Contains(m2.recordText, strings.Repeat("r", 300)) {
		t.Error("record text should hold the full field value")
	}

	// A key the record doesn't own is ignored — the selection must not move.
	newModel, _ = m2.Update(keyMsg("t"))
	m3 := newModel.(*model)
	if !m3.recordActive || m3.eventsTableMode {
		t.Error("stray keys must not reach the browser under the record view")
	}

	newModel, _ = m3.Update(keyMsg("v"))
	m4 := newModel.(*model)
	if m4.recordActive {
		t.Error("v should close the record view")
	}
}
