package cwtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// defaultLookback is the event-query window used until the user picks another
// one (p) or passes --since. It matches the tool's historical 24-hour scan.
const defaultLookback = 24 * time.Hour

// lookbackPresets are the query windows the "p" key cycles through. Narrower
// windows make FilterLogEvents scan (and bill) less data, so busy groups get
// faster, cheaper queries.
var lookbackPresets = []time.Duration{
	30 * time.Minute,
	time.Hour,
	3 * time.Hour,
	6 * time.Hour,
	12 * time.Hour,
	24 * time.Hour,
	3 * 24 * time.Hour,
	7 * 24 * time.Hour,
}

// nextLookback returns the preset after cur, wrapping past the last one. A cur
// that is not a preset (e.g. a custom --since) advances to the first preset
// larger than it, so repeated presses always walk the full cycle.
func nextLookback(cur time.Duration) time.Duration {
	for i, p := range lookbackPresets {
		if p == cur {
			return lookbackPresets[(i+1)%len(lookbackPresets)]
		}
	}
	for _, p := range lookbackPresets {
		if p > cur {
			return p
		}
	}
	return lookbackPresets[0]
}

// formatLookback renders a query window compactly for the panel, status bar
// and hints: whole days as "3d", whole hours as "24h", whole minutes as "30m".
// A day-multiple below 48h stays in hours so the default reads "24h".
func formatLookback(d time.Duration) string {
	day := 24 * time.Hour
	switch {
	case d > day && d%day == 0:
		return fmt.Sprintf("%dd", d/day)
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Minute == 0 && d != 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	default:
		return d.String()
	}
}

// ParseLookback parses a user-supplied query window such as "30m", "2h" or
// "3d". Day suffixes are handled here because time.ParseDuration stops at
// hours. The window must be positive.
func ParseLookback(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty duration")
	}
	var d time.Duration
	if n := strings.TrimSuffix(s, "d"); n != s {
		days, err := time.ParseDuration(n + "h")
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (use e.g. 30m, 2h, 3d)", s)
		}
		d = days * 24
	} else {
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (use e.g. 30m, 2h, 3d)", s)
		}
	}
	if d <= 0 {
		return 0, fmt.Errorf("duration %q must be positive", s)
	}
	return d, nil
}

// maxEventCell caps how many runes of a log message go into a table cell. The
// shared table sizes each column to its widest cell, so an unclipped megabyte
// message would blow up layout; the full text stays reachable via Enter (full
// log viewer) and y (copy).
const maxEventCell = 160

// clipEventCell flattens a log message onto one line and truncates it for a
// table cell, marking the cut with an ellipsis.
func clipEventCell(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
	r := []rune(s)
	if len(r) <= maxEventCell {
		return s
	}
	return string(r[:maxEventCell-1]) + "…"
}

// eventTableColumns returns the column set for the events table. The stream
// column only appears in group-level search, where events interleave from many
// streams and the origin matters.
func eventTableColumns(withStream bool) []table.Column {
	cols := []table.Column{{Title: "Time", Width: 4}}
	if withStream {
		cols = append(cols, table.Column{Title: "Stream", Width: 4})
	}
	return append(cols, table.Column{Title: "Message", Width: 4})
}

// eventTableRows maps events onto rows matching eventTableColumns. Timestamps
// include the date because the query window can span days.
func eventTableRows(events []types.FilteredLogEvent, withStream bool) []table.Row {
	rows := make([]table.Row, 0, len(events))
	for _, ev := range events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		row := table.Row{t.Format("2006-01-02 15:04:05.000")}
		if withStream {
			row = append(row, clipEventCell(aws.ToString(ev.LogStreamName)))
		}
		rows = append(rows, append(row, clipEventCell(aws.ToString(ev.Message))))
	}
	return rows
}

// buildEventsTable (re)creates the shared-widget table from the current
// events, preserving the selection. Called when table mode turns on and when
// a fresh event batch lands while it is on.
func (m *model) buildEventsTable() {
	withStream := m.groupLevelSearch
	m.eventsTable = table.New(
		table.WithColumns(eventTableColumns(withStream)),
		table.WithRows(eventTableRows(m.events, withStream)),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1), // pin the time column when scrolling wide messages
	)
	m.eventsTable.SetCursor(m.selectedEventIdx)
}

// syncEventsTableCursor moves the table cursor to selectedEventIdx using the
// table's own movement functions so the viewport follows the selection (a bare
// SetCursor does not scroll).
func (m *model) syncEventsTableCursor() {
	cur := m.eventsTable.Cursor()
	want := m.selectedEventIdx
	switch {
	case want == cur:
	case want > cur:
		m.eventsTable.MoveDown(want - cur)
	case want == 0:
		m.eventsTable.GotoTop()
	default:
		m.eventsTable.MoveUp(cur - want)
	}
}
