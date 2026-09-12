package cwtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/mattn/go-runewidth"

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

// maxEventCell caps how many runes of a value go into a NON-wrapping table
// cell (stream names, JSON field values). The shared table sizes each column
// to its widest cell, so an unclipped megabyte value would blow up layout. The
// Message column is not subject to this: it is sized to the space actually
// left on screen and wraps (see messageColumnWidth / wrapEventMessage).
const maxEventCell = 160

// tableCellPadding is the left+right padding the shared table adds to every
// cell, and tableScrollGutter the width of its vertical scrollbar column.
// Layout math uses these instead of assuming — the widget owns the real
// numbers, and guessing them is how the message column ends up the wrong size.
const (
	tableCellPadding  = 2
	tableScrollGutter = 2
)

// minMessageWidth is the narrowest the Message column is allowed to get before
// the terminal is simply too narrow; below this the column stops shrinking and
// the table scrolls horizontally instead.
const minMessageWidth = 24

// maxWrapLines caps how many lines one event's message may occupy. A 40 KB
// JSON blob would otherwise fill the page by itself; past the cap the last
// line says how much was left, and v / Enter show the value in full.
// maxWrapLinesJSON is the more generous cap that applies while J has expanded
// the JSON: the reader asked to see the document, so it gets more room — but
// still a bound, still labelled when it bites.
const (
	maxWrapLines     = 8
	maxWrapLinesJSON = 40
)

// fittedWidth returns the width the shared table will give a column: the wider
// of its header and its widest cell. Mirrors table.fitColumns so the builder
// can work out what room is left for the Message column before handing the
// table anything.
func fittedWidth(header string, cells []string) int {
	w := runewidth.StringWidth(header)
	for _, c := range cells {
		if cw := runewidth.StringWidth(c); cw > w {
			w = cw
		}
	}
	return w
}

// messageColumnWidth returns how wide the Message column should be so the
// table fills its panel exactly: everything left over after the other columns,
// their padding and the scrollbar gutter. avail <= 0 means the caller doesn't
// know the width yet (a build before the first WindowSizeMsg), in which case
// the column falls back to the legacy cap rather than collapsing.
func messageColumnWidth(avail int, otherWidths []int) int {
	if avail <= 0 {
		return maxEventCell
	}
	used := tableScrollGutter
	for _, w := range otherWidths {
		used += w + tableCellPadding
	}
	return max(minMessageWidth, avail-used-tableCellPadding)
}

// wrapEventMessage lays a log message out across the Message column: newlines
// in the message start new lines, and any line too wide is wrapped at word
// boundaries, hard-breaking a token (an ARN, a JSON blob) that has no spaces
// to break at. The result is capped at maxWrapLines, and the cap is never
// silent — the final line reports how many lines were dropped.
func wrapEventMessage(msg string, width, maxLines int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(strings.TrimRight(msg, "\n"), "\n") {
		para = sanitizeLogLine(strings.ReplaceAll(strings.ReplaceAll(para, "\r", ""), "\t", "    "))
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapWords(para, width)...)
	}
	if len(out) == 0 {
		return []string{""}
	}
	if maxLines > 0 && len(out) > maxLines {
		dropped := len(out) - maxLines
		out = out[:maxLines]
		note := fmt.Sprintf("… +%d more line(s) — v for the full record", dropped)
		out[maxLines-1] = runewidth.Truncate(note, width, "…")
	}
	return out
}

// wrapWords breaks one line at the last space that fits and mid-token when a
// token (an ARN, a JSON blob) has no space to break at. Spacing inside the
// line is preserved exactly — log output is often column-aligned or indented,
// and collapsing runs of spaces would quietly rewrite the message.
func wrapWords(line string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(line)
	var out []string
	for start := 0; start < len(runes); {
		if runewidth.StringWidth(string(runes[start:])) <= width {
			out = append(out, string(runes[start:]))
			break
		}
		// Walk forward while the line still fits, remembering the last point
		// we could break at without splitting a word.
		cut, lastSpace, w := start, -1, 0
		for i := start; i < len(runes); i++ {
			rw := runewidth.RuneWidth(runes[i])
			if w+rw > width {
				break
			}
			w += rw
			cut = i + 1
			if runes[i] == ' ' {
				lastSpace = i + 1
			}
		}
		if lastSpace > start {
			cut = lastSpace // prefer the word boundary
		}
		if cut == start {
			cut = start + 1 // a single rune wider than the column
		}
		out = append(out, strings.TrimRight(string(runes[start:cut]), " "))
		start = cut
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// flattenEventText puts a log message on one line for a table cell.
func flattenEventText(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

// eventTableColumns returns the table's column set: Time and Message. The
// stream is deliberately absent even in a whole-group search — it would cost
// most of the width the message needs, and the record view (v) names it for
// the selected event.
func eventTableColumns() []table.Column {
	return []table.Column{
		{Title: "Time", Width: 4},
		{Title: "Message", Width: 4},
	}
}

// eventTimestamp renders an event's timestamp for the table; the date is
// included because the query window can span days.
func eventTimestamp(ev types.FilteredLogEvent) string {
	t := time.Unix(0, aws.ToInt64(ev.Timestamp)*int64(time.Millisecond))
	return t.Format("2006-01-02 15:04:05.000")
}

// wrappedRows lays out one event as one or more table rows: the first carries
// the fixed cells (time, stream, any JSON fields) plus the message's first
// line, and each continuation row leaves those cells blank so the wrapped text
// lines up underneath the message column. groups maps every row back to its
// event so the table selects, stripes and navigates by event, not by line.
func wrappedRows(fixed []string, msgLines []string) []table.Row {
	rows := make([]table.Row, 0, len(msgLines))
	for i, line := range msgLines {
		row := make(table.Row, len(fixed), len(fixed)+1)
		if i == 0 {
			copy(row, fixed)
		}
		rows = append(rows, append(row, line))
	}
	return rows
}

// eventTableRows maps events onto rows matching eventTableColumns, wrapping
// each message to msgWidth. With formatJSON on, an embedded JSON payload is
// expanded first, so the cell shows the indented document. The second return
// value is the row→event mapping.
func eventTableRows(events []types.FilteredLogEvent, formatJSON bool, msgWidth int) ([]table.Row, []int) {
	rows := make([]table.Row, 0, len(events))
	groups := make([]int, 0, len(events))
	for i, ev := range events {
		fixed := []string{eventTimestamp(ev)}
		msg := aws.ToString(ev.Message)
		lineCap := maxWrapLines
		if formatJSON {
			// Expanding is an explicit request to see the document, so it gets
			// more room than a message that merely happens to be long.
			msg = prettifyJSON(msg)
			lineCap = maxWrapLinesJSON
		}
		evRows := wrappedRows(fixed, wrapEventMessage(msg, msgWidth, lineCap))
		for range evRows {
			groups = append(groups, i)
		}
		rows = append(rows, evRows...)
	}
	return rows, groups
}

// eventsTableWidth is the width the events-panel table is rendered at — the
// right-hand panel minus the border padding renderEventsPanel applies. The
// builder needs it up front so the Message column can claim the leftover
// width; 0 before the first WindowSizeMsg, which the builder handles.
func (m *model) eventsTableWidth() int {
	if m.width <= 0 {
		return 0
	}
	return m.streamsPanelWidth() - 2
}

// buildEventsTable (re)creates the shared-widget table from the current
// events, preserving the selection. Called when table mode turns on, when the
// J (format JSON) toggle flips, on resize, and when a fresh event batch lands
// while it is on.
func (m *model) buildEventsTable() {
	data := buildEventTableData(m.events, m.eventsJSON, m.eventsTableWidth())
	m.eventsTable = table.New(
		table.WithColumns(data.cols),
		table.WithRows(data.rows),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1),       // pin the time column while scrolling
		table.WithRowGroups(data.groups), // a wrapped message stays one event
	)
	m.eventsTable.SetWidth(m.eventsTableWidth())
	m.eventsTable.SetCursorGroup(m.selectedEventIdx)
}

// syncEventsTableCursor moves the table cursor to selectedEventIdx using the
// table's own movement functions so the viewport follows the selection (a bare
// SetCursor does not scroll).
func (m *model) syncEventsTableCursor() {
	// Compare in events, not rendered rows: a wrapped message spans several
	// rows, so row arithmetic would drift from the selected event.
	cur := m.eventsTable.CursorGroup()
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
