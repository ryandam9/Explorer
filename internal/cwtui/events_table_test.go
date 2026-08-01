package cwtui

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestNextLookbackCyclesPresets(t *testing.T) {
	cases := []struct {
		cur, want time.Duration
	}{
		{30 * time.Minute, time.Hour},
		{time.Hour, 3 * time.Hour},
		{24 * time.Hour, 3 * 24 * time.Hour},
		{7 * 24 * time.Hour, 30 * time.Minute},  // wraps
		{45 * time.Minute, time.Hour},           // custom --since lands on next-larger preset
		{30 * 24 * time.Hour, 30 * time.Minute}, // beyond the largest preset wraps to the first
	}
	for _, c := range cases {
		if got := nextLookback(c.cur); got != c.want {
			t.Errorf("nextLookback(%v) = %v, want %v", c.cur, got, c.want)
		}
	}
}

func TestFormatLookback(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Minute, "30m"},
		{90 * time.Minute, "90m"},
		{time.Hour, "1h"},
		{24 * time.Hour, "24h"}, // the default reads in hours, matching the docs
		{3 * 24 * time.Hour, "3d"},
		{7 * 24 * time.Hour, "7d"},
	}
	for _, c := range cases {
		if got := formatLookback(c.d); got != c.want {
			t.Errorf("formatLookback(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestParseLookback(t *testing.T) {
	good := []struct {
		in   string
		want time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"2h", 2 * time.Hour},
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{" 45m ", 45 * time.Minute},
	}
	for _, c := range good {
		got, err := ParseLookback(c.in)
		if err != nil {
			t.Errorf("ParseLookback(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLookback(%q) = %v, want %v", c.in, got, c.want)
		}
	}
	for _, in := range []string{"", "abc", "-5m", "0", "0d", "-2d", "dd"} {
		if _, err := ParseLookback(in); err == nil {
			t.Errorf("ParseLookback(%q) succeeded, want error", in)
		}
	}
}

func TestEventTableColumns(t *testing.T) {
	cols := eventTableColumns(false)
	if len(cols) != 2 || cols[0].Title != "Time" || cols[1].Title != "Message" {
		t.Errorf("without stream: got %+v", cols)
	}
	cols = eventTableColumns(true)
	if len(cols) != 3 || cols[1].Title != "Stream" {
		t.Errorf("with stream: got %+v", cols)
	}
}

func TestEventTableRows(t *testing.T) {
	ts := time.Date(2026, 8, 1, 10, 30, 0, 0, time.Local).UnixMilli()
	events := []types.FilteredLogEvent{
		{
			Timestamp:     aws.Int64(ts),
			LogStreamName: aws.String("stream-a"),
			Message:       aws.String("line one\nline two\twith tab"),
		},
		{
			Timestamp: aws.Int64(ts),
			Message:   aws.String(strings.Repeat("x", 500)),
		},
	}

	rows := eventTableRows(events, true)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	wantTime := time.UnixMilli(ts).Format("2006-01-02 15:04:05.000")
	if rows[0][0] != wantTime {
		t.Errorf("time cell = %q, want %q", rows[0][0], wantTime)
	}
	if rows[0][1] != "stream-a" {
		t.Errorf("stream cell = %q", rows[0][1])
	}
	if want := "line one line two with tab"; rows[0][2] != want {
		t.Errorf("message cell = %q, want newlines/tabs flattened to %q", rows[0][2], want)
	}

	// Long messages are clipped for the cell (the widget sizes columns to the
	// widest cell) and marked with an ellipsis.
	clipped := rows[1][2]
	if got := len([]rune(clipped)); got != maxEventCell {
		t.Errorf("clipped cell runes = %d, want %d", got, maxEventCell)
	}
	if !strings.HasSuffix(clipped, "…") {
		t.Errorf("clipped cell should end in ellipsis: %q", clipped)
	}

	// Without the stream column each row is just Time + Message.
	rows = eventTableRows(events, false)
	if len(rows[0]) != 2 {
		t.Errorf("row width without stream = %d, want 2", len(rows[0]))
	}
}

// The table cursor must follow selectedEventIdx through wraps in both
// directions so Enter/y always act on the highlighted row.
func TestSyncEventsTableCursor(t *testing.T) {
	m := &model{}
	for i := 0; i < 5; i++ {
		m.events = append(m.events, types.FilteredLogEvent{
			Timestamp: aws.Int64(1700000000000),
			Message:   aws.String("m"),
		})
	}
	m.buildEventsTable()

	for _, want := range []int{1, 2, 4, 0, 3, 0, 4} {
		m.selectedEventIdx = want
		m.syncEventsTableCursor()
		if got := m.eventsTable.Cursor(); got != want {
			t.Errorf("cursor = %d, want %d", got, want)
		}
	}
}
