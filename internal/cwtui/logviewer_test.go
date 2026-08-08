package cwtui

import (
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
)

func testEvent(id string, ts int64, msg string) types.FilteredLogEvent {
	return types.FilteredLogEvent{
		EventId:   aws.String(id),
		Timestamp: aws.Int64(ts),
		Message:   aws.String(msg),
	}
}

func TestWrapLine(t *testing.T) {
	short := wrapLine("hello", 80, "    ")
	if len(short) != 1 || short[0] != "hello" {
		t.Errorf("short line should not wrap, got %v", short)
	}

	long := wrapLine(strings.Repeat("a", 50), 20, "    ")
	if len(long) < 2 {
		t.Fatalf("long line should wrap, got %v", long)
	}
	if len([]rune(long[0])) != 20 {
		t.Errorf("first wrapped chunk should be width 20, got %d", len([]rune(long[0])))
	}
	if !strings.HasPrefix(long[1], "    ") {
		t.Errorf("continuation should be indented, got %q", long[1])
	}
}

func TestSanitizeLogLine(t *testing.T) {
	if got := sanitizeLogLine("plain ascii line"); got != "plain ascii line" {
		t.Errorf("clean line should pass through unchanged, got %q", got)
	}
	if got := sanitizeLogLine("col1\tcol2\r"); got != "col1    col2" {
		t.Errorf("tabs should expand and CR drop, got %q", got)
	}
}

func TestWrapLineCellWidths(t *testing.T) {
	// 30 CJK runes are 60 terminal cells; a rune-counted wrap would emit
	// segments twice as wide as the row and the renderer would clip them.
	wide := strings.Repeat("界", 30)
	out := wrapLine(wide, 20, "    ")
	if len(out) < 2 {
		t.Fatalf("wide line should wrap, got %v", out)
	}
	for i, seg := range out {
		if w := runewidth.StringWidth(seg); w > 20 {
			t.Errorf("segment %d is %d cells wide, must be ≤ 20: %q", i, w, seg)
		}
		if i > 0 && !strings.HasPrefix(seg, "    ") {
			t.Errorf("continuation %d should be indented, got %q", i, seg)
		}
	}
}

func TestViewerRebuildSanitizesTabsAndWidths(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 40}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "goroutine 1:\n\tat main.go:10\tin main.main"),
		testEvent("e2", 2000, strings.Repeat("宽", 40)),
	})
	for i, line := range v.lines {
		if strings.ContainsAny(line, "\t\r") {
			t.Errorf("line %d still contains tab/CR: %q", i, line)
		}
		if w := runewidth.StringWidth(line); w > 40 {
			t.Errorf("line %d is %d cells wide, must be ≤ wrapW 40: %q", i, w, line)
		}
	}
}

func TestViewerAppendDedupsAndTracksTimestamp(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 80}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "first"),
		testEvent("e2", 2000, "second"),
	})
	if len(v.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(v.events))
	}
	if v.lastTS != 2000 {
		t.Errorf("lastTS = %d, want 2000", v.lastTS)
	}

	// Re-delivering e2 (overlapping fetch window) must not duplicate it.
	v.append([]types.FilteredLogEvent{
		testEvent("e2", 2000, "second"),
		testEvent("e3", 3000, "third"),
	})
	if len(v.events) != 3 {
		t.Errorf("expected 3 events after dedup, got %d", len(v.events))
	}
	if v.lastTS != 3000 {
		t.Errorf("lastTS = %d, want 3000", v.lastTS)
	}
}

func TestViewerRebuildMultilineMessages(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 120}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "line one\nline two\nline three"),
	})
	if len(v.lines) != 3 {
		t.Fatalf("expected 3 display lines, got %d: %v", len(v.lines), v.lines)
	}
	if !strings.Contains(v.lines[0], "line one") {
		t.Errorf("first line should carry message start, got %q", v.lines[0])
	}
	if !strings.HasPrefix(v.lines[0], "[") {
		t.Errorf("first line should carry timestamp prefix, got %q", v.lines[0])
	}
	if !strings.HasPrefix(v.lines[1], "    ") {
		t.Errorf("continuation lines should be indented, got %q", v.lines[1])
	}
}

func TestViewerSearchMatches(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 120}
	v.append([]types.FilteredLogEvent{
		testEvent("e1", 1000, "starting worker"),
		testEvent("e2", 2000, "ERROR something broke"),
		testEvent("e3", 3000, "recovered from error state"),
		testEvent("e4", 4000, "all good"),
	})

	v.term = "error"
	v.computeMatches()
	if len(v.matches) != 2 {
		t.Fatalf("case-insensitive search expected 2 matches, got %d", len(v.matches))
	}

	// nextMatch cycles forward and wraps.
	first := v.nextMatch(1)
	second := v.nextMatch(1)
	wrapped := v.nextMatch(1)
	if first == second || wrapped != v.matches[v.matchIdx] {
		t.Errorf("nextMatch should cycle: first=%d second=%d wrapped=%d", first, second, wrapped)
	}

	// jumpToFirstMatchFrom picks the first match at/after the given line.
	line := v.jumpToFirstMatchFrom(v.matches[1])
	if line != v.matches[1] {
		t.Errorf("jumpToFirstMatchFrom = %d, want %d", line, v.matches[1])
	}

	// Clearing the term clears matches.
	v.term = ""
	v.computeMatches()
	if len(v.matches) != 0 {
		t.Errorf("expected no matches with empty term, got %d", len(v.matches))
	}
}

func TestViewerScrollAndFollow(t *testing.T) {
	v := &logViewer{seen: map[string]bool{}, wrapW: 120}
	for i := 0; i < 50; i++ {
		v.append([]types.FilteredLogEvent{testEvent(strings.Repeat("i", i+1), int64(i*1000), "event")})
	}
	if len(v.lines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(v.lines))
	}

	bodyH := 10
	v.scrollToBottom(bodyH)
	if v.offset != 40 {
		t.Errorf("scrollToBottom offset = %d, want 40", v.offset)
	}

	v.scrollBy(100, bodyH)
	if v.offset != 40 {
		t.Errorf("scrollBy should clamp to max offset 40, got %d", v.offset)
	}

	v.scrollBy(-100, bodyH)
	if v.offset != 0 {
		t.Errorf("scrollBy should clamp to 0, got %d", v.offset)
	}

	v.centerOn(25, bodyH)
	if v.offset != 20 {
		t.Errorf("centerOn(25) offset = %d, want 20", v.offset)
	}
}

func TestModelOpensViewerOnEnter(t *testing.T) {
	m := &model{
		width:  100,
		height: 30,
		focus:  focusEvents,
		view:   viewEvents,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: "us-east-1"},
		},
		filteredStreams: []types.LogStream{
			{LogStreamName: aws.String("stream-1")},
		},
		events:      []types.FilteredLogEvent{testEvent("e1", 1000, "hello")},
		eventSearch: textinput.New(),
		viewer:      logViewer{search: textinput.New()},
	}

	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := newModel.(*model)
	if !m2.viewer.active {
		t.Fatal("Enter on an event should open the full log viewer")
	}
	if m2.viewer.key.stream != "stream-1" || m2.viewer.key.region != "us-east-1" {
		t.Errorf("viewer key = %+v, want stream-1/us-east-1", m2.viewer.key)
	}
	if !m2.viewer.follow || !m2.viewer.loading {
		t.Errorf("viewer should open following and loading, got follow=%v loading=%v",
			m2.viewer.follow, m2.viewer.loading)
	}
	if cmd == nil {
		t.Error("opening the viewer should issue load + tick commands")
	}

	// Esc closes the viewer.
	newModel, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := newModel.(*model)
	if m3.viewer.active {
		t.Error("Esc should close the viewer")
	}
}

func TestViewerEventsMsgDroppedWhenStale(t *testing.T) {
	m := &model{
		viewer: logViewer{
			active: true,
			key:    viewerKey{region: "us-east-1", group: "g", stream: "s"},
			seen:   map[string]bool{},
			wrapW:  80,
		},
	}

	stale := viewerEventsMsg{
		key:    viewerKey{region: "us-east-1", group: "g", stream: "other"},
		events: []types.FilteredLogEvent{testEvent("e1", 1000, "stale")},
	}
	newModel, _ := m.Update(stale)
	m2 := newModel.(*model)
	if len(m2.viewer.events) != 0 {
		t.Errorf("stale viewer events should be dropped, got %d", len(m2.viewer.events))
	}

	fresh := viewerEventsMsg{
		key:     m2.viewer.key,
		initial: true,
		events:  []types.FilteredLogEvent{testEvent("e1", 1000, "fresh")},
	}
	newModel, _ = m2.Update(fresh)
	m3 := newModel.(*model)
	if len(m3.viewer.events) != 1 || m3.viewer.loading {
		t.Errorf("fresh viewer events should apply, got events=%d loading=%v",
			len(m3.viewer.events), m3.viewer.loading)
	}
}

func TestViewerTickStopsWhenClosed(t *testing.T) {
	m := &model{
		viewer: logViewer{
			active: false,
			key:    viewerKey{region: "us-east-1", group: "g"},
		},
	}
	_, cmd := m.Update(viewerTickMsg{key: m.viewer.key})
	if cmd != nil {
		t.Error("tick for a closed viewer should not re-arm streaming")
	}
}

// viewerModel builds a model with an open viewer holding the given events,
// the shared fixture for the table-mode tests.
func viewerModel(stream string, events ...types.FilteredLogEvent) *model {
	m := &model{
		width:  120,
		height: 40,
		viewer: logViewer{
			active:     true,
			key:        viewerKey{region: "us-east-1", group: "g", stream: stream},
			seen:       map[string]bool{},
			wrapW:      100,
			tableSplit: true,
		},
	}
	m.viewer.append(events)
	return m
}

func TestViewerTableModeToggle(t *testing.T) {
	m := viewerModel("s1",
		testEvent("e1", 1000, "first"),
		testEvent("e2", 2000, "second"),
	)

	newModel, _ := m.Update(keyMsg("t"))
	m2 := newModel.(*model)
	if !m2.viewer.tableMode {
		t.Fatal("t should switch the viewer to table mode")
	}
	if got := len(m2.viewer.table.Rows()); got != 2 {
		t.Errorf("table should hold one row per event, got %d", got)
	}

	newModel, _ = m2.Update(keyMsg("t"))
	if newModel.(*model).viewer.tableMode {
		t.Error("t again should return to the log view")
	}
}

func TestViewerTableModeBlockedByGrep(t *testing.T) {
	m := viewerModel("s1", testEvent("e1", 1000, "first"))
	m.viewer.setGrep("first")

	newModel, _ := m.Update(keyMsg("t"))
	m2 := newModel.(*model)
	if m2.viewer.tableMode {
		t.Error("t must not enter table mode while a grep filter is applied")
	}
	if m2.toast == "" {
		t.Error("blocking t should explain itself via a toast")
	}
}

func TestViewerTableFollowsNewEvents(t *testing.T) {
	m := viewerModel("s1", testEvent("e1", 1000, "first"))
	m.viewer.tableMode = true
	m.viewer.follow = true
	m.viewer.rebuildTable()

	m.viewer.append([]types.FilteredLogEvent{
		testEvent("e2", 2000, "second"),
		testEvent("e3", 3000, "third"),
	})
	if cur := m.viewer.table.Cursor(); cur != 2 {
		t.Errorf("following table should pin the cursor to the newest row, got %d", cur)
	}

	// With follow off, the cursor stays where the user left it.
	m.viewer.follow = false
	m.viewer.table.GotoTop()
	m.viewer.table.MoveDown(1)
	m.viewer.append([]types.FilteredLogEvent{testEvent("e4", 4000, "fourth")})
	if cur := m.viewer.table.Cursor(); cur != 1 {
		t.Errorf("paused table should keep the cursor at 1, got %d", cur)
	}
}

func TestViewerTableStreamColumnOnlyForGroup(t *testing.T) {
	ev := testEvent("e1", 1000, "hello")
	ev.LogStreamName = aws.String("stream-a")

	group := viewerModel("", ev) // whole-group viewer
	group.viewer.tableMode = true
	group.viewer.rebuildTable()
	single := viewerModel("stream-a", ev)
	single.viewer.tableMode = true
	single.viewer.rebuildTable()

	hasStream := func(m *model) bool {
		for _, c := range m.viewer.table.Columns() {
			if c.Title == "Stream" {
				return true
			}
		}
		return false
	}
	if !hasStream(group) {
		t.Error("whole-group table should include the Stream column")
	}
	if hasStream(single) {
		t.Error("single-stream table should not include the Stream column")
	}
}

func TestViewerTableOpensRecordView(t *testing.T) {
	m := viewerModel("s1",
		testEvent("e1", 1000, "first message"),
		testEvent("e2", 2000, "second message"),
	)
	m.viewer.tableMode = true
	m.viewer.rebuildTable()
	m.viewer.follow = true
	m.viewer.table.GotoBottom()

	newModel, _ := m.Update(keyMsg("v"))
	m2 := newModel.(*model)
	if !m2.recordActive {
		t.Fatal("v in viewer table mode should open the record view")
	}
	if !strings.Contains(m2.recordText, "second message") {
		t.Errorf("record should show the highlighted row's event, got %q", m2.recordText)
	}

	// Esc closes the record and leaves the viewer open.
	newModel, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m3 := newModel.(*model)
	if m3.recordActive {
		t.Error("Esc should close the record view")
	}
	if !m3.viewer.active {
		t.Error("closing the record must leave the viewer open")
	}
}

func TestFormatEvents(t *testing.T) {
	out := formatEvents([]types.FilteredLogEvent{
		testEvent("e1", 1700000000000, "hello world"),
	})
	if !strings.Contains(out, "hello world") {
		t.Errorf("formatted output should contain the message, got %q", out)
	}
	if !strings.HasPrefix(out, "[") || !strings.HasSuffix(out, "\n") {
		t.Errorf("formatted output should be '[ts] msg\\n' lines, got %q", out)
	}
}
