package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// streamEvent is a matched event as a group-wide FilterLogEvents returns it:
// with the stream it came from.
func streamEvent(id, stream string, ts int64) types.FilteredLogEvent {
	return types.FilteredLogEvent{
		EventId:       aws.String(id),
		LogStreamName: aws.String(stream),
		Timestamp:     aws.Int64(ts),
		Message:       aws.String("ERROR boom"),
	}
}

func TestAggregateStreamMatches(t *testing.T) {
	got := aggregateStreamMatches([]types.FilteredLogEvent{
		streamEvent("e1", "stream-a", 3000),
		streamEvent("e2", "stream-b", 1000),
		streamEvent("e3", "stream-a", 1000),
		streamEvent("e4", "stream-a", 2000),
		streamEvent("e5", "stream-b", 5000),
	})

	if len(got) != 2 {
		t.Fatalf("got %d streams, want 2", len(got))
	}

	// Newest activity first: stream-b's last match (5000) beats stream-a's (3000).
	if got[0].Stream != "stream-b" || got[1].Stream != "stream-a" {
		t.Errorf("order = %q, %q; want stream-b, stream-a", got[0].Stream, got[1].Stream)
	}
	if got[0].Count != 2 || got[0].First != 1000 || got[0].Last != 5000 {
		t.Errorf("stream-b = %+v, want count 2, first 1000, last 5000", got[0])
	}
	if got[1].Count != 3 || got[1].First != 1000 || got[1].Last != 3000 {
		t.Errorf("stream-a = %+v, want count 3, first 1000, last 3000", got[1])
	}
}

// Equal last-match timestamps sort by name, so two refreshes of the same data
// don't shuffle the rows under the cursor.
func TestAggregateStreamMatchesStableOnTies(t *testing.T) {
	got := aggregateStreamMatches([]types.FilteredLogEvent{
		streamEvent("e1", "zebra", 1000),
		streamEvent("e2", "alpha", 1000),
		streamEvent("e3", "mango", 1000),
	})
	var names []string
	for _, sm := range got {
		names = append(names, sm.Stream)
	}
	if strings.Join(names, ",") != "alpha,mango,zebra" {
		t.Errorf("tie order = %v, want alpha,mango,zebra", names)
	}
}

// A matched event with no stream name is counted under a label that reads as
// unknown — never dropped, and never attributed to a real stream.
func TestAggregateStreamMatchesKeepsUnnamedStream(t *testing.T) {
	got := aggregateStreamMatches([]types.FilteredLogEvent{
		{EventId: aws.String("e1"), Timestamp: aws.Int64(1000), Message: aws.String("x")},
	})
	if len(got) != 1 {
		t.Fatalf("got %d streams, want 1", len(got))
	}
	if got[0].Stream != unknownStreamLabel {
		t.Errorf("stream = %q, want %q", got[0].Stream, unknownStreamLabel)
	}
	if got[0].Count != 1 {
		t.Errorf("count = %d, want 1", got[0].Count)
	}
}

func TestAggregateStreamMatchesEmpty(t *testing.T) {
	if got := aggregateStreamMatches(nil); len(got) != 0 {
		t.Errorf("got %d streams for no events, want 0", len(got))
	}
}

// A truncated sweep undercounts, so every tally is rendered as a lower bound.
func TestStreamMatchFormatCountMarksTruncation(t *testing.T) {
	exact := &streamMatchState{}
	if got := exact.formatCount(42); got != "42" {
		t.Errorf("exact count = %q, want %q", got, "42")
	}

	capped := &streamMatchState{truncated: true}
	if got := capped.formatCount(42); got != "≥42" {
		t.Errorf("truncated count = %q, want %q", got, "≥42")
	}
}

func TestFormatMatchTimeNoDataIsNotZeroTime(t *testing.T) {
	if got := formatMatchTime(0); got != "—" {
		t.Errorf("formatMatchTime(0) = %q, want the no-data dash", got)
	}
	if got := formatMatchTime(1_700_000_000_000); got == "—" {
		t.Errorf("a real timestamp rendered as no-data")
	}
}

// An empty pattern would tally the whole window, which answers nothing — the
// prompt says so instead of firing the query.
func TestRunStreamMatchRejectsEmptyPattern(t *testing.T) {
	m := newStreamMatchTestModel()
	m.streamMatch.prompting = true
	m.streamMatch.input.SetValue("   ")

	var cmds []tea.Cmd
	m.runStreamMatch(&cmds)

	if len(cmds) != 0 {
		t.Errorf("queued %d commands for an empty pattern, want 0", len(cmds))
	}
	if m.streamMatch.loading {
		t.Errorf("entered the loading state for an empty pattern")
	}
	if m.streamMatch.errNote == "" {
		t.Errorf("no note explaining why nothing ran")
	}
}

// A result for a group the user has navigated away from is dropped, so a stale
// answer can't describe the wrong group.
func TestHandleStreamMatchResultDropsStaleGroup(t *testing.T) {
	m := newStreamMatchTestModel()
	m.streamMatch.group = "/aws/lambda/current"
	m.streamMatch.pattern = "ERROR"
	m.streamMatch.loading = true

	var cmds []tea.Cmd
	m.handleStreamMatchResult(streamMatchMsg{
		group:   "/aws/lambda/previous",
		pattern: "ERROR",
		matches: []StreamMatch{{Stream: "s1", Count: 1}},
	}, &cmds)

	if m.streamMatch.active || len(m.streamMatch.matches) != 0 {
		t.Errorf("stale result applied: active=%v matches=%d", m.streamMatch.active, len(m.streamMatch.matches))
	}
	if !m.streamMatch.loading {
		t.Errorf("loading cleared by a stale result")
	}
}

// The mode's own keys are handled while it shows; anything else falls through
// so the global bindings keep working.
func TestHandleStreamMatchKeysOwnership(t *testing.T) {
	m := newStreamMatchTestModel()
	m.streamMatch.active = true
	m.streamMatch.matches = []StreamMatch{{Stream: "s1"}, {Stream: "s2"}}
	m.rebuildStreamMatchTable()

	var cmds []tea.Cmd
	if !m.handleStreamMatchKeys(tea.KeyMsg{Type: tea.KeyDown}, &cmds) {
		t.Errorf("down arrow not handled by the results view")
	}
	if m.streamMatch.idx != 1 {
		t.Errorf("cursor = %d, want 1", m.streamMatch.idx)
	}

	// Not the mode's key: it must fall through to the global handler.
	if m.handleStreamMatchKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}, &cmds) {
		t.Errorf("D was swallowed by the results view")
	}

	// Esc clears the tally entirely.
	if !m.handleStreamMatchKeys(tea.KeyMsg{Type: tea.KeyEsc}, &cmds) {
		t.Errorf("esc not handled")
	}
	if m.streamMatch.visible() || len(m.streamMatch.matches) != 0 {
		t.Errorf("esc left the mode showing")
	}
}

// Opening a matched stream carries the pattern into the events panel, so the
// drill-down lands on the very lines that were counted.
func TestOpenMatchedStreamCarriesPattern(t *testing.T) {
	m := newStreamMatchTestModel()
	m.streams = []types.LogStream{
		{LogStreamName: aws.String("stream-a")},
		{LogStreamName: aws.String("stream-b")},
	}
	m.filteredStreams = m.streams
	m.streamMatch.active = true
	m.streamMatch.pattern = "ERROR"
	m.streamMatch.matches = []StreamMatch{{Stream: "stream-b", Count: 3}}
	m.streamMatch.idx = 0

	var cmds []tea.Cmd
	m.openMatchedStream(&cmds)

	if m.selectedStreamIdx != 1 {
		t.Errorf("selected stream index = %d, want 1 (stream-b)", m.selectedStreamIdx)
	}
	if m.eventSearch.Value() != "ERROR" {
		t.Errorf("event pattern = %q, want ERROR", m.eventSearch.Value())
	}
	if m.view != viewEvents || m.focus != focusEvents {
		t.Errorf("did not move to the events panel: view=%v focus=%v", m.view, m.focus)
	}
	if m.groupLevelSearch {
		t.Errorf("group-level search left on; the drill-down targets one stream")
	}
	if len(cmds) == 0 {
		t.Errorf("no fetch queued for the opened stream")
	}
}

// newStreamMatchTestModel builds a model with just the fields these tests
// touch — no AWS client, since none of them reach the network.
func newStreamMatchTestModel() *model {
	return &model{
		width:       120,
		height:      40,
		view:        viewStreams,
		focus:       focusStreams,
		lookback:    defaultLookback,
		eventSearch: textinput.New(),
		streamMatch: streamMatchState{input: textinput.New()},
	}
}
