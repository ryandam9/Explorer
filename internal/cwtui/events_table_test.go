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

	rows := eventTableRows(events, true, 0)
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
	rows = eventTableRows(events, false, 0)
	if len(rows[0]) != 2 {
		t.Errorf("row width without stream = %d, want 2", len(rows[0]))
	}
}

// ←/→ pan the message window across the full text; ellipses mark text hidden
// off either edge so a partial view is never mistaken for the whole message.
func TestEventMessageCellPanning(t *testing.T) {
	long := strings.Repeat("abcdefghij", 50) // 500 runes

	if got := eventMessageCell("short", 0); got != "short" {
		t.Errorf("unshifted short = %q", got)
	}

	// Unshifted long: head window with trailing ellipsis.
	got := eventMessageCell(long, 0)
	if !strings.HasPrefix(got, "abcdefghij") || !strings.HasSuffix(got, "…") {
		t.Errorf("unshifted long = %q", got)
	}

	// Mid-pan: both edges elided, window starts at the shift offset.
	got = eventMessageCell(long, 40)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("mid-pan should be elided on both edges: %q", got)
	}
	if want := string([]rune(long)[40:50]); !strings.Contains(got, want) {
		t.Errorf("mid-pan window should start at rune 40: %q", got)
	}
	if n := len([]rune(got)); n != maxEventCell {
		t.Errorf("mid-pan cell runes = %d, want %d", n, maxEventCell)
	}

	// Panned to the tail: leading ellipsis only, remainder shown in full.
	got = eventMessageCell(long, 460)
	if want := "…" + string([]rune(long)[460:]); got != want {
		t.Errorf("tail pan = %q, want %q", got, want)
	}

	// Panned past a shorter message: a bare ellipsis, not an empty cell.
	if got := eventMessageCell("tiny", 40); got != "…" {
		t.Errorf("past-the-end pan = %q, want …", got)
	}
}

func TestClampMsgShift(t *testing.T) {
	cases := []struct {
		shift, maxLen, want int
	}{
		{0, 500, 0},
		{40, 500, 40},
		{480, 500, 460}, // keep the last msgShiftStep runes reachable
		{400, 100, 60},  // events replaced by shorter ones: pull the pan back
		{40, 0, 0},      // no events
		{-10, 500, 0},   // never negative
		{40, 20, 0},     // maxLen below one step: no panning possible
	}
	for _, c := range cases {
		if got := clampMsgShift(c.shift, c.maxLen); got != c.want {
			t.Errorf("clampMsgShift(%d, %d) = %d, want %d", c.shift, c.maxLen, got, c.want)
		}
	}
}

// panEventsTable must pan the message window right and retrace left, with the
// pan bounded by the longest message.
func TestPanEventsTable(t *testing.T) {
	m := &model{}
	m.events = []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1700000000000), Message: aws.String(strings.Repeat("x", 100))},
		{Timestamp: aws.Int64(1700000000000), Message: aws.String("short")},
	}
	m.buildEventsTable()

	m.panEventsTable(true)
	if m.msgShift != msgShiftStep {
		t.Fatalf("after one right pan msgShift = %d, want %d", m.msgShift, msgShiftStep)
	}
	// The longest message is 100 runes, so the pan stops at 100-40=60.
	for i := 0; i < 5; i++ {
		m.panEventsTable(true)
	}
	if m.msgShift != 60 {
		t.Errorf("pan should stop at 60 (maxLen-step), got %d", m.msgShift)
	}
	m.panEventsTable(false)
	m.panEventsTable(false)
	if m.msgShift != 0 {
		t.Errorf("panning back should reach 0, got %d", m.msgShift)
	}
	m.panEventsTable(false) // at 0: no-op (no hidden columns to unscroll)
	if m.msgShift != 0 {
		t.Errorf("msgShift went negative: %d", m.msgShift)
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
