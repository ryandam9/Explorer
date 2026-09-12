package cwtui

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/mattn/go-runewidth"
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

	const msgW = 40
	rows, groups := eventTableRows(events, true, msgW)
	if len(rows) != len(groups) {
		t.Fatalf("rows = %d but groups = %d; every row needs an event", len(rows), len(groups))
	}

	wantTime := time.UnixMilli(ts).Format("2006-01-02 15:04:05.000")
	if rows[0][0] != wantTime {
		t.Errorf("time cell = %q, want %q", rows[0][0], wantTime)
	}
	if rows[0][1] != "stream-a" {
		t.Errorf("stream cell = %q", rows[0][1])
	}

	// A message with an embedded newline occupies one row per line, and only
	// the first carries the fixed cells so the text aligns under the column.
	if rows[0][2] != "line one" {
		t.Errorf("first message line = %q, want %q", rows[0][2], "line one")
	}
	if rows[1][2] != "line two    with tab" {
		t.Errorf("second message line = %q (tabs expand, no flattening)", rows[1][2])
	}
	if rows[1][0] != "" || rows[1][1] != "" {
		t.Errorf("continuation row repeated the fixed cells: %q / %q", rows[1][0], rows[1][1])
	}
	if groups[0] != 0 || groups[1] != 0 {
		t.Errorf("both lines should map to event 0, got %v", groups[:2])
	}

	// A long message wraps to the column width instead of being cut off.
	second := 0
	for i, g := range groups {
		if g == 1 {
			second = i
			break
		}
	}
	if second == 0 {
		t.Fatalf("no rows produced for the second event: groups %v", groups)
	}
	wrapped := rows[second:]
	if len(wrapped) < 2 {
		t.Errorf("500-rune message produced %d row(s) at width %d, want it wrapped", len(wrapped), msgW)
	}
	for i, r := range wrapped {
		if w := runewidth.StringWidth(r[2]); w > msgW {
			t.Errorf("wrapped line %d is %d wide, want <= %d: %q", i, w, msgW, r[2])
		}
	}

	// Without the stream column each row is just Time + Message.
	rows, _ = eventTableRows(events, false, msgW)
	if len(rows[0]) != 2 {
		t.Errorf("row width without stream = %d, want 2", len(rows[0]))
	}
}

// The Message column takes whatever the other columns leave, so a wide
// terminal is filled rather than stopping at a fixed width.
func TestMessageColumnWidth(t *testing.T) {
	// Time (23) + its padding, plus the scrollbar gutter, come off the top.
	got := messageColumnWidth(200, []int{23})
	if want := 200 - tableScrollGutter - (23 + tableCellPadding) - tableCellPadding; got != want {
		t.Errorf("messageColumnWidth = %d, want %d", got, want)
	}

	// A narrow terminal stops shrinking at the floor rather than collapsing.
	if got := messageColumnWidth(30, []int{23, 40}); got != minMessageWidth {
		t.Errorf("narrow width = %d, want the %d floor", got, minMessageWidth)
	}

	// Width unknown (before the first resize): fall back, don't collapse.
	if got := messageColumnWidth(0, []int{23}); got != maxEventCell {
		t.Errorf("unknown width = %d, want the %d fallback", got, maxEventCell)
	}
}

func TestWrapEventMessage(t *testing.T) {
	// Wraps at word boundaries when it can.
	got := wrapEventMessage("the quick brown fox jumps", 12, 0)
	for _, line := range got {
		if runewidth.StringWidth(line) > 12 {
			t.Errorf("line %q exceeds width 12", line)
		}
	}
	if strings.Join(got, " ") != "the quick brown fox jumps" {
		t.Errorf("word wrap lost or reordered text: %v", got)
	}

	// Hard-breaks a token with nowhere to break (an ARN, a JSON blob).
	long := strings.Repeat("z", 25)
	got = wrapEventMessage(long, 10, 0)
	if len(got) != 3 {
		t.Errorf("unbreakable token wrapped to %d lines, want 3: %v", len(got), got)
	}
	if strings.Join(got, "") != long {
		t.Errorf("hard break lost characters: %v", got)
	}

	// The line cap is never silent about what it dropped.
	many := strings.Repeat("word ", 200)
	got = wrapEventMessage(many, 20, 3)
	if len(got) != 3 {
		t.Fatalf("capped output = %d lines, want 3", len(got))
	}
	if !strings.Contains(got[2], "more line") {
		t.Errorf("last line %q should say how much was dropped", got[2])
	}

	// An empty message still yields exactly one line, so its row exists.
	if got := wrapEventMessage("", 20, 4); len(got) != 1 || got[0] != "" {
		t.Errorf("empty message = %v, want one empty line", got)
	}
}

// ←/→ scroll the column window (the split-JSON layout can be wider than the
// panel); the message itself wraps, so there is nothing to pan it past.
func TestPanEventsTable(t *testing.T) {
	m := &model{width: 100, height: 30}
	m.events = []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1), Message: aws.String(`{"a":"1","b":"2","c":"3"}`)},
	}
	m.jsonSplit = true
	m.eventsTableMode = true
	m.buildEventsTable()

	before, _ := m.eventsTable.ColScrollInfo()
	m.panEventsTable(true)
	afterRight, _ := m.eventsTable.ColScrollInfo()
	if afterRight < before {
		t.Errorf("panning right scrolled columns backwards: %d then %d", before, afterRight)
	}
	m.panEventsTable(false)
	if got, _ := m.eventsTable.ColScrollInfo(); got != before {
		t.Errorf("left then right did not retrace: %d, want %d", got, before)
	}
}

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

// The reported bug: on a wide monitor the table stopped at a fixed width and
// left the right-hand side of the panel empty. The Message column must take
// whatever the other columns leave, whether messages are short or long.
func TestEventsTableFillsAvailableWidth(t *testing.T) {
	events := []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1), Message: aws.String("short")},
		{Timestamp: aws.Int64(2), Message: aws.String("also short")},
	}

	for _, avail := range []int{120, 200, 320} {
		d := buildEventTableData(events, false, false, avail)
		msgCol := d.cols[len(d.cols)-1]
		if msgCol.Title != "Message" {
			t.Fatalf("last column is %q, want Message", msgCol.Title)
		}

		timeW := fittedWidth("Time", []string{eventTimestamp(events[0])})
		want := avail - tableScrollGutter - (timeW + tableCellPadding) - tableCellPadding
		if msgCol.Width != want {
			t.Errorf("avail %d: message column = %d, want %d (the whole remainder)", avail, msgCol.Width, want)
		}

		// End to end: the widget grows the fixed columns to their content, so
		// the rendered table must occupy exactly the width it was given.
		used := tableScrollGutter + timeW + tableCellPadding + msgCol.Width + tableCellPadding
		if used != avail {
			t.Errorf("avail %d: table occupies %d columns, leaving %d unused", avail, used, avail-used)
		}
	}
}

// A message longer than the column wraps onto continuation rows mapped back to
// their event, instead of being truncated with an ellipsis.
func TestEventsTableWrapsLongMessagesIntoRows(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta ", 30)
	events := []types.FilteredLogEvent{
		{Timestamp: aws.Int64(1), Message: aws.String("one liner")},
		{Timestamp: aws.Int64(2), Message: aws.String(long)},
	}

	d := buildEventTableData(events, false, false, 120)
	if len(d.rows) != len(d.groups) {
		t.Fatalf("rows = %d, groups = %d", len(d.rows), len(d.groups))
	}
	if len(d.rows) <= len(events) {
		t.Errorf("rows = %d for %d events; the long message should have wrapped", len(d.rows), len(events))
	}

	msgIdx := len(d.cols) - 1
	msgW := d.cols[msgIdx].Width
	for i, r := range d.rows {
		if w := runewidth.StringWidth(r[msgIdx]); w > msgW {
			t.Errorf("row %d message is %d wide, past the %d column", i, w, msgW)
		}
	}

	// Continuation rows leave the time cell empty so the text lines up under
	// the message column.
	for i := 1; i < len(d.rows); i++ {
		if d.groups[i] == d.groups[i-1] && d.rows[i][0] != "" {
			t.Errorf("continuation row %d repeated the time cell %q", i, d.rows[i][0])
		}
	}
}
