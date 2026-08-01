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
// message would blow up layout; ←/→ pan the window across the full text, which
// also stays reachable via Enter (full log viewer) and y (copy).
const maxEventCell = 160

// msgShiftStep is how many runes one ←/→ press pans the message window by.
const msgShiftStep = 40

// flattenEventText puts a log message on one line for a table cell.
func flattenEventText(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// clipEventCell flattens a value and truncates it for a table cell, marking
// the cut with an ellipsis. Used for cells that don't pan (stream names).
func clipEventCell(s string) string {
	return eventMessageCell(s, 0)
}

// eventMessageCell renders the visible window of a message cell: the text is
// flattened, shifted left by `shift` runes and capped at maxEventCell. Leading
// and trailing ellipses mark text panned off either edge, so a truncated cell
// is never mistaken for the whole message.
func eventMessageCell(s string, shift int) string {
	r := []rune(flattenEventText(s))
	if shift <= 0 {
		if len(r) <= maxEventCell {
			return string(r)
		}
		return string(r[:maxEventCell-1]) + "…"
	}
	if shift >= len(r) {
		return "…" // fully panned past this (shorter) message
	}
	w := r[shift:]
	if len(w) <= maxEventCell-1 {
		return "…" + string(w)
	}
	return "…" + string(w[:maxEventCell-2]) + "…"
}

// maxEventMsgLen returns the longest flattened message length, the bound for
// how far the message window can pan.
func maxEventMsgLen(events []types.FilteredLogEvent) int {
	longest := 0
	for _, ev := range events {
		if n := len([]rune(flattenEventText(aws.ToString(ev.Message)))); n > longest {
			longest = n
		}
	}
	return longest
}

// clampMsgShift keeps the pan offset useful after events change: never
// negative, and never so far right that even the longest message has panned
// out of view (at least the last msgShiftStep runes stay visible).
func clampMsgShift(shift, maxLen int) int {
	if maxShift := maxLen - msgShiftStep; shift > maxShift {
		shift = maxShift
	}
	if shift < 0 {
		return 0
	}
	return shift
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

// eventTableRows maps events onto rows matching eventTableColumns, with the
// message column panned msgShift runes to the left. Timestamps include the
// date because the query window can span days.
func eventTableRows(events []types.FilteredLogEvent, withStream bool, msgShift int) []table.Row {
	rows := make([]table.Row, 0, len(events))
	for _, ev := range events {
		t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
		row := table.Row{t.Format("2006-01-02 15:04:05.000")}
		if withStream {
			row = append(row, clipEventCell(aws.ToString(ev.LogStreamName)))
		}
		rows = append(rows, append(row, eventMessageCell(aws.ToString(ev.Message), msgShift)))
	}
	return rows
}

// buildEventsTable (re)creates the shared-widget table from the current
// events, preserving the selection. Called when table mode turns on and when
// a fresh event batch lands while it is on.
func (m *model) buildEventsTable() {
	withStream := m.groupLevelSearch
	m.maxMsgLen = maxEventMsgLen(m.events)
	m.msgShift = clampMsgShift(m.msgShift, m.maxMsgLen)
	m.eventsTable = table.New(
		table.WithColumns(eventTableColumns(withStream)),
		table.WithRows(eventTableRows(m.events, withStream, m.msgShift)),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1), // pin the time column while panning wide messages
	)
	m.eventsTable.SetCursor(m.selectedEventIdx)
}

// refreshEventsTableRows re-renders the rows for a new pan offset without
// recreating the table, so the cursor and scroll position stay put.
func (m *model) refreshEventsTableRows() {
	m.eventsTable.SetRows(eventTableRows(m.events, m.groupLevelSearch, m.msgShift))
}

// panEventsTable handles ←/→ in table mode. Right first reveals hidden
// columns (group search can push the message column off), then pans the
// message window; left pans back before un-scrolling columns, so the two
// directions retrace each other.
func (m *model) panEventsTable(right bool) {
	if right {
		if _, hiddenRight := m.eventsTable.ColScrollInfo(); hiddenRight > 0 {
			m.eventsTable.ScrollRight()
			return
		}
		if shifted := clampMsgShift(m.msgShift+msgShiftStep, m.maxMsgLen); shifted != m.msgShift {
			m.msgShift = shifted
			m.refreshEventsTableRows()
		}
		return
	}
	if m.msgShift > 0 {
		m.msgShift = clampMsgShift(m.msgShift-msgShiftStep, m.maxMsgLen)
		m.refreshEventsTableRows()
		return
	}
	m.eventsTable.ScrollLeft()
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
