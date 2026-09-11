package cwtui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// streamMatchMaxScan bounds how many events one "which streams match?" sweep
// holds while counting. The sweep reads the whole query window, so this is a
// memory guard, not an answer-shaping limit — and when it bites the counts
// become lower bounds, which the UI says out loud ("≥") instead of passing
// them off as totals.
const streamMatchMaxScan = 50000

// StreamMatch is one stream's tally from a group-wide pattern search: how many
// events matched and when the first and last of them landed.
type StreamMatch struct {
	Stream string
	Count  int
	First  int64 // ms
	Last   int64 // ms
}

// unknownStreamLabel stands in when an event arrives without a stream name.
// The AWS API always sets it on a group-wide FilterLogEvents, but a matched
// event is never dropped just because its label is missing (§1/§6a) — it is
// counted under a name that reads as unknown rather than as a real stream.
const unknownStreamLabel = "(unknown stream)"

// aggregateStreamMatches tallies matched events by stream, newest activity
// first (ties broken by name so the order is stable across refreshes). It is a
// pure function over what the API returned — the whole analysis is here, so it
// can be fixture-tested without AWS.
func aggregateStreamMatches(events []types.FilteredLogEvent) []StreamMatch {
	byStream := make(map[string]*StreamMatch)
	for _, ev := range events {
		name := aws.ToString(ev.LogStreamName)
		if name == "" {
			name = unknownStreamLabel
		}
		ts := aws.ToInt64(ev.Timestamp)
		sm, ok := byStream[name]
		if !ok {
			byStream[name] = &StreamMatch{Stream: name, Count: 1, First: ts, Last: ts}
			continue
		}
		sm.Count++
		if ts < sm.First {
			sm.First = ts
		}
		if ts > sm.Last {
			sm.Last = ts
		}
	}

	out := make([]StreamMatch, 0, len(byStream))
	for _, sm := range byStream {
		out = append(out, *sm)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Last != out[j].Last {
			return out[i].Last > out[j].Last
		}
		return out[i].Stream < out[j].Stream
	})
	return out
}

// MatchingStreams answers "which streams in this group contain the pattern?"
// with ONE group-wide FilterLogEvents sweep per pattern — the stream name comes
// back on every event, so the tally is a client-side fold rather than a query
// per stream.
//
// truncated reports that the sweep hit streamMatchMaxScan, so the counts are
// lower bounds and a quiet stream may be missing entirely; the caller must
// surface it. Failed patterns degrade the result like every other multi-pattern
// fetch: what succeeded is tallied, with the error returned alongside.
func (c *CWLogsClient) MatchingStreams(ctx context.Context, region, logGroupName string, patterns []string, startMillis int64) ([]StreamMatch, bool, error) {
	events, truncated, err := c.GetAllLogEventsSinceMulti(ctx, region, logGroupName, "", patterns, startMillis, streamMatchMaxScan)
	return aggregateStreamMatches(events), truncated, err
}

// ---- model integration -------------------------------------------------------

type streamMatchMsg struct {
	group     string
	region    string
	pattern   string
	matches   []StreamMatch
	truncated bool
	err       error
}

// streamMatchState is the [2] Log streams panel's "which streams match?" mode:
// the prompt, the tally it produced, and what the tally is an answer to. The
// window and pattern are captured at query time so the header describes the
// results on screen, not whatever `p` was cycled to afterwards.
type streamMatchState struct {
	prompting bool
	loading   bool
	active    bool // results are showing

	input   textinput.Model
	pattern string        // pattern(s) the showing results answer
	window  time.Duration // query window those results covered
	group   string        // group the results belong to

	matches   []StreamMatch
	truncated bool
	errNote   string

	tbl table.Model
	idx int
}

// reset clears the mode entirely — used when the target group changes or C
// clears every filter, so a tally can never outlive what it described.
func (s *streamMatchState) reset() {
	s.prompting = false
	s.loading = false
	s.active = false
	s.input.SetValue("")
	s.pattern = ""
	s.group = ""
	s.matches = nil
	s.truncated = false
	s.errNote = ""
	s.idx = 0
}

// visible reports whether the mode owns the streams panel's body right now.
func (s *streamMatchState) visible() bool {
	return s.prompting || s.loading || s.active
}

// selected returns the stream the cursor is on, if any.
func (s *streamMatchState) selected() (StreamMatch, bool) {
	if !s.active || s.idx < 0 || s.idx >= len(s.matches) {
		return StreamMatch{}, false
	}
	return s.matches[s.idx], true
}

// openStreamMatch ("F" on the streams panel) starts a group-wide stream search,
// seeding the prompt with the active event pattern so the two searches compose
// instead of fighting.
func (m *model) openStreamMatch() {
	grp, ok := m.selectedGroup()
	if !ok {
		return
	}
	m.streamMatch.prompting = true
	m.streamMatch.group = aws.ToString(grp.LogGroupName)
	if m.streamMatch.input.Value() == "" {
		m.streamMatch.input.SetValue(m.eventSearch.Value())
	}
	m.streamMatch.input.CursorEnd()
	m.streamMatch.input.Focus()
}

// runStreamMatch fires the sweep for the typed pattern.
func (m *model) runStreamMatch(cmds *[]tea.Cmd) {
	pattern := strings.TrimSpace(m.streamMatch.input.Value())
	if pattern == "" {
		// An empty pattern would tally every event in the window — expensive
		// and meaningless as an answer to "where does this string appear?".
		// Checked before anything else so the prompt always explains itself.
		m.streamMatch.errNote = "Enter a string or pattern to search for"
		return
	}
	grp, ok := m.selectedGroup()
	if !ok {
		m.streamMatch.errNote = "No log group selected"
		return
	}

	m.streamMatch.prompting = false
	m.streamMatch.loading = true
	m.streamMatch.active = false
	m.streamMatch.errNote = ""
	m.streamMatch.matches = nil
	m.streamMatch.truncated = false
	m.streamMatch.idx = 0
	m.streamMatch.pattern = pattern
	m.streamMatch.window = m.lookback
	m.streamMatch.group = aws.ToString(grp.LogGroupName)

	region := grp.Region
	group := m.streamMatch.group
	since := time.Now().Add(-m.lookback).UnixMilli()
	*cmds = append(*cmds, func() tea.Msg {
		matches, truncated, err := m.client.MatchingStreams(m.ctx, region, group, SplitPatterns(pattern), since)
		return streamMatchMsg{group: group, region: region, pattern: pattern, matches: matches, truncated: truncated, err: err}
	})
}

// handleStreamMatchResult applies a finished sweep, dropping a stale answer for
// a group the user has already navigated away from.
func (m *model) handleStreamMatchResult(msg streamMatchMsg, cmds *[]tea.Cmd) {
	if msg.group != m.streamMatch.group || msg.pattern != m.streamMatch.pattern {
		return
	}
	m.streamMatch.loading = false
	m.streamMatch.active = true
	m.streamMatch.matches = msg.matches
	m.streamMatch.truncated = msg.truncated
	m.streamMatch.idx = 0
	if msg.err != nil {
		// Partial results still show — with the failure named, never silently.
		m.streamMatch.errNote = clipToastText(msg.err.Error())
		m.setToast("Stream search failed: " + m.streamMatch.errNote)
		*cmds = append(*cmds, toastCmd(4*time.Second))
	}
	m.rebuildStreamMatchTable()
}

// handleStreamMatchKeys owns the keys while the mode is showing. Everything it
// does not own falls through to the panel underneath.
func (m *model) handleStreamMatchKeys(msg tea.KeyMsg, cmds *[]tea.Cmd) (handled bool) {
	s := &m.streamMatch

	if s.prompting {
		switch msg.String() {
		case "enter":
			m.runStreamMatch(cmds)
		case "esc":
			s.prompting = false
			if !s.active {
				s.reset()
			}
		default:
			var cmd tea.Cmd
			s.input, cmd = s.input.Update(msg)
			*cmds = append(*cmds, cmd)
			s.errNote = ""
		}
		return true
	}

	if s.loading {
		if msg.String() == "esc" {
			s.reset()
		}
		return true
	}

	if !s.active {
		return false
	}

	switch msg.String() {
	case "esc", "backspace":
		s.reset()
	case "F":
		m.openStreamMatch()
	case "up", "k":
		if s.idx > 0 {
			s.idx--
			s.tbl.MoveUp(1)
		}
	case "down", "j":
		if s.idx < len(s.matches)-1 {
			s.idx++
			s.tbl.MoveDown(1)
		}
	case "enter":
		m.openMatchedStream(cmds)
	default:
		return false
	}
	return true
}

// openMatchedStream drills from a tallied stream into its events, carrying the
// pattern across so the events panel shows the very lines that were counted.
func (m *model) openMatchedStream(cmds *[]tea.Cmd) {
	sel, ok := m.streamMatch.selected()
	if !ok || sel.Stream == unknownStreamLabel {
		return
	}

	// Point the stream list at the tallied stream. The list is the events
	// panel's source of truth for "which stream", so a stream hidden by the
	// name filter is revealed rather than silently opening the wrong one.
	idx := -1
	for i, st := range m.filteredStreams {
		if aws.ToString(st.LogStreamName) == sel.Stream {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.streamSearch.SetValue("")
		m.filterStreams()
		for i, st := range m.filteredStreams {
			if aws.ToString(st.LogStreamName) == sel.Stream {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		// The stream has events in the window but isn't in the listing (the
		// stream list is itself capped) — say so instead of opening whatever
		// happens to be selected.
		m.setToast("Stream not in the loaded stream list: " + sel.Stream)
		*cmds = append(*cmds, toastCmd(4*time.Second))
		return
	}

	m.selectedStreamIdx = idx
	m.eventSearch.SetValue(m.streamMatch.pattern)
	m.streamMatch.active = false // keep the pattern; drop the results overlay
	m.groupLevelSearch = false
	m.watchMode = false
	m.eventsLoading = true
	m.view = viewEvents
	m.focus = focusEvents
	*cmds = append(*cmds, m.loadEventsCmd())
}

// rebuildStreamMatchTable (re)builds the results table through the shared
// widget, so this panel looks and pans like every other grid in the app.
func (m *model) rebuildStreamMatchTable() {
	width := m.streamsPanelWidth()
	timeW := 19
	countW := 10
	// Reserve the gutters the widget draws so the stream column can't overrun
	// the panel and wrap (measure, don't assume).
	nameW := width - countW - 2*timeW - 10
	if nameW < 20 {
		nameW = 20
	}

	cols := []table.Column{
		{Title: "Stream", Width: nameW},
		{Title: "Matches", Width: countW},
		{Title: "First match", Width: timeW},
		{Title: "Last match", Width: timeW},
	}

	rows := make([]table.Row, 0, len(m.streamMatch.matches))
	for _, sm := range m.streamMatch.matches {
		rows = append(rows, table.Row{
			sm.Stream,
			m.streamMatch.formatCount(sm.Count),
			formatMatchTime(sm.First),
			formatMatchTime(sm.Last),
		})
	}

	m.streamMatch.tbl = table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithStyles(ui.TableStylesZebra()),
		table.WithFrozenColumns(1),
	)
	if m.streamMatch.idx > 0 {
		m.streamMatch.tbl.MoveDown(m.streamMatch.idx)
	}
}

// formatCount renders a tally, marking it as a lower bound when the sweep was
// truncated — a capped count must never read as a total.
func (s *streamMatchState) formatCount(n int) string {
	if s.truncated {
		return "≥" + fmt.Sprint(n)
	}
	return fmt.Sprint(n)
}

func formatMatchTime(ms int64) string {
	if ms == 0 {
		return "—"
	}
	return time.Unix(0, ms*int64(time.Millisecond)).Format("2006-01-02 15:04:05")
}

// renderStreamMatch draws the mode's body inside the streams panel. It returns
// the body only; the panel's heading and border belong to the caller.
func (m *model) renderStreamMatch(width int) string {
	s := &m.streamMatch
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAccent()))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning()))

	var b strings.Builder

	if s.prompting {
		b.WriteString(" Streams containing: " + s.input.View() + "\n")
		if s.errNote != "" {
			b.WriteString(" " + warn.Render(s.errNote) + "\n")
		} else {
			b.WriteString(mutedStyle.Render("  CloudWatch filter syntax; ; ORs several. Enter searches every stream · Esc cancels") + "\n")
		}
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  Searches the whole group over the last %s (p changes the window).", formatLookback(m.lookback))) + "\n")
		return b.String()
	}

	b.WriteString(" Streams containing " + accent.Render(s.pattern) +
		mutedStyle.Render(fmt.Sprintf("  · last %s", formatLookback(s.window))) + "\n")

	if s.loading {
		b.WriteString(mutedStyle.Render("  F edits · Esc cancels") + "\n\n")
		b.WriteString(fmt.Sprintf("  %s Searching every stream in the group…\n", m.spinner.View()))
		return b.String()
	}

	// The tally answers only for the window it covered, and only for streams
	// that had a match in it — never let that read as "this string is nowhere
	// else" (§8: an unknown is not a no).
	// "N of M" only when M is known — the stream list loads independently, and
	// "12 of 0" would be worse than no denominator at all.
	summary := fmt.Sprintf("  %d streams matched", len(s.matches))
	if total := len(m.streams); total > 0 {
		summary = fmt.Sprintf("  %d of %d streams matched", len(s.matches), total)
	}
	if s.truncated {
		summary += warn.Render(fmt.Sprintf("  ! scan capped at %d events — counts are minimums, quiet streams may be missing", streamMatchMaxScan))
	}
	b.WriteString(mutedStyle.Render(summary) + "\n")
	b.WriteString(mutedStyle.Render("  Enter opens the stream with the pattern applied · F edits · Esc clears") + "\n")
	b.WriteString("\n")

	if s.errNote != "" {
		b.WriteString(" " + warn.Render("Partial results — "+s.errNote) + "\n")
	}

	if len(s.matches) == 0 {
		b.WriteString(fmt.Sprintf("  No stream matched in the last %s. A wider window (p) may.\n", formatLookback(s.window)))
		return b.String()
	}

	bodyH := m.height - 10
	if bodyH < 3 {
		bodyH = 3
	}
	s.tbl.SetWidth(width - 2)
	s.tbl.SetHeight(bodyH)
	b.WriteString(s.tbl.View() + "\n")
	if ind := ui.TableScrollIndicator(&s.tbl); ind != "" {
		b.WriteString(" " + ind)
	}
	return b.String()
}
