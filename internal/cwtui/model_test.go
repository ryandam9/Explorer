package cwtui

import (
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{5, 3, 5},
		{-1, 10, 10},
		{0, 0, 0},
	}
	for _, tt := range tests {
		if got := max(tt.a, tt.b); got != tt.want {
			t.Errorf("max(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestGetVisibleRange(t *testing.T) {
	tests := []struct {
		current    int
		total      int
		maxVisible int
		wantStart  int
		wantEnd    int
	}{
		{0, 5, 10, 0, 5},
		{2, 20, 5, 0, 5},
		{10, 20, 5, 8, 13},
		{18, 20, 5, 15, 20},
	}
	for _, tt := range tests {
		start, end := getVisibleRange(tt.current, tt.total, tt.maxVisible)
		if start != tt.wantStart || end != tt.wantEnd {
			t.Errorf("getVisibleRange(%d, %d, %d) = (%d, %d), want (%d, %d)",
				tt.current, tt.total, tt.maxVisible, start, end, tt.wantStart, tt.wantEnd)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"/aws/lambda/my-function", "_aws_lambda_my-function"},
		{"some stream name:with-colons", "some_stream_name-with-colons"},
		{"simple-name", "simple-name"},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.input); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestModelCycleFocus(t *testing.T) {
	m := &model{
		view:  viewStreams,
		focus: focusGroups,
	}

	// viewStreams cycles between focusGroups and focusStreams
	m.cycleFocus(true)
	if m.focus != focusStreams {
		t.Errorf("cycleFocus(true) from focusGroups with viewStreams = %v, want focusStreams", m.focus)
	}

	m.cycleFocus(true)
	if m.focus != focusGroups {
		t.Errorf("cycleFocus(true) from focusStreams with viewStreams = %v, want focusGroups", m.focus)
	}

	m.cycleFocus(false)
	if m.focus != focusStreams {
		t.Errorf("cycleFocus(false) from focusGroups with viewStreams = %v, want focusStreams", m.focus)
	}

	// viewEvents cycles between focusGroups and focusEvents
	m.view = viewEvents
	m.focus = focusGroups

	m.cycleFocus(true)
	if m.focus != focusEvents {
		t.Errorf("cycleFocus(true) from focusGroups with viewEvents = %v, want focusEvents", m.focus)
	}

	m.cycleFocus(true)
	if m.focus != focusGroups {
		t.Errorf("cycleFocus(true) from focusEvents with viewEvents = %v, want focusGroups", m.focus)
	}

	m.cycleFocus(false)
	if m.focus != focusEvents {
		t.Errorf("cycleFocus(false) from focusGroups with viewEvents = %v, want focusEvents", m.focus)
	}
}

func TestModelNavigateList(t *testing.T) {
	m := &model{
		focus: focusGroups,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("group1")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("group2")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("group3")}, Region: "us-east-1"},
		},
		selectedGroupIdx: 0,
	}

	m.navigateList(1)
	if m.selectedGroupIdx != 1 {
		t.Errorf("navigateList(1) = %d, want 1", m.selectedGroupIdx)
	}

	m.navigateList(1)
	if m.selectedGroupIdx != 2 {
		t.Errorf("navigateList(1) = %d, want 2", m.selectedGroupIdx)
	}

	// Wrap around
	m.navigateList(1)
	if m.selectedGroupIdx != 0 {
		t.Errorf("navigateList(1) wrapped around to = %d, want 0", m.selectedGroupIdx)
	}

	m.navigateList(-1)
	if m.selectedGroupIdx != 2 {
		t.Errorf("navigateList(-1) wrapped around to = %d, want 2", m.selectedGroupIdx)
	}
}

func TestModelFilterGroupsAndStreams(t *testing.T) {
	gSearch := textinput.New()
	sSearch := textinput.New()

	m := &model{
		groups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn1")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn2")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/ecs/service")}, Region: "us-east-1"},
		},
		streams: []types.LogStream{
			{LogStreamName: aws.String("stream-2026-06-11-01")},
			{LogStreamName: aws.String("stream-2026-06-11-02")},
			{LogStreamName: aws.String("stderr-stream")},
		},
		groupSearch:  gSearch,
		streamSearch: sSearch,
	}

	// Empty searches should return everything
	m.filterGroups()
	if len(m.filteredGroups) != 3 {
		t.Errorf("expected 3 filtered groups, got %d", len(m.filteredGroups))
	}

	m.filterStreams()
	if len(m.filteredStreams) != 3 {
		t.Errorf("expected 3 filtered streams, got %d", len(m.filteredStreams))
	}

	// Filter groups with keyword
	m.groupSearch.SetValue("lambda")
	m.filterGroups()
	if len(m.filteredGroups) != 2 {
		t.Errorf("expected 2 filtered groups with 'lambda', got %d", len(m.filteredGroups))
	}

	// Filter streams with keyword
	m.streamSearch.SetValue("stderr")
	m.filterStreams()
	if len(m.filteredStreams) != 1 {
		t.Errorf("expected 1 filtered stream with 'stderr', got %d", len(m.filteredStreams))
	}
	if aws.ToString(m.filteredStreams[0].LogStreamName) != "stderr-stream" {
		t.Errorf("expected stream name 'stderr-stream', got %q", aws.ToString(m.filteredStreams[0].LogStreamName))
	}
}

func TestModelToast(t *testing.T) {
	m := &model{}
	m.setToast("Hello")
	if m.toast != "Hello" {
		t.Errorf("toast = %q, want 'Hello'", m.toast)
	}
	if m.toastExp.Before(time.Now()) {
		t.Error("toast expiration time should be in the future")
	}
}

func TestModelUpdateKeys(t *testing.T) {
	m := &model{
		focus: focusGroups,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("g1")}, Region: "us-east-1"},
		},
		groupSearch: textinput.New(),
	}

	// Test basic key handlers
	newModel, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m2 := newModel.(*model)
	if m2.focus != focusStreams {
		t.Errorf("expected focus to change to focusStreams, got %v", m2.focus)
	}

	// Escape with watchMode active should deactivate it
	m2.watchMode = true
	newModel, cmd = m2.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m3 := newModel.(*model)
	if m3.watchMode {
		t.Error("expected watchMode to be deactivated on Escape key")
	}
	_ = cmd
}

func TestModelUpdateMsgTypes(t *testing.T) {
	m := &model{}

	// Test clearToastMsg
	newModel, _ := m.Update(clearToastMsg{})
	m2 := newModel.(*model)
	if m2.toast != "" {
		t.Errorf("expected clearToastMsg to clear toast, got %q", m2.toast)
	}

	// Test groupsMsg error path
	someErr := testingError("some api error")
	newModel, _ = m.Update(groupsMsg{err: someErr})
	m3 := newModel.(*model)
	if m3.err != someErr {
		t.Errorf("expected error to be saved, got %v", m3.err)
	}

	// Test groupsMsg success path
	groups := []LogGroup{{LogGroup: types.LogGroup{LogGroupName: aws.String("g1")}, Region: "us-east-1"}}
	newModel, _ = m.Update(groupsMsg{groups: groups})
	m4 := newModel.(*model)
	if len(m4.groups) != 1 {
		t.Errorf("expected groups to load, got %d", len(m4.groups))
	}
}

type testingError string

func (e testingError) Error() string {
	return string(e)
}

func groupSearchModel() *model {
	m := &model{
		focus: focusGroups,
		view:  viewStreams,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: "us-east-1"},
		},
		groupSearch:  textinput.New(),
		streamSearch: textinput.New(),
		eventSearch:  textinput.New(),
		lookback:     defaultLookback,
	}
	return m
}

func TestGroupSearchPromptsWhenNoPattern(t *testing.T) {
	m := groupSearchModel()

	// G with no pattern set must prompt, not dump every stream's events.
	newModel, _ := m.Update(keyMsg("G"))
	m2 := newModel.(*model)
	if !m2.groupLevelSearch || m2.view != viewEvents {
		t.Fatal("G should enter group-level search on the events view")
	}
	if !m2.eventSearchActive {
		t.Error("G with no pattern should open the pattern prompt")
	}
	if m2.eventsLoading {
		t.Error("no query may run before the prompt is answered")
	}

	// Enter on the (still empty) prompt explicitly browses everything.
	newModel, cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := newModel.(*model)
	if m3.eventSearchActive || !m3.eventsLoading || cmd == nil {
		t.Error("Enter on the prompt should run the query")
	}
}

func TestGroupSearchWithPatternQueriesImmediately(t *testing.T) {
	m := groupSearchModel()
	m.eventSearch.SetValue("ERROR")

	newModel, cmd := m.Update(keyMsg("G"))
	m2 := newModel.(*model)
	if m2.eventSearchActive {
		t.Error("G with a pattern already set should not re-prompt")
	}
	if !m2.eventsLoading || cmd == nil {
		t.Error("G with a pattern should query immediately")
	}
}

func TestGroupSearchPromptEscBacksOut(t *testing.T) {
	m := groupSearchModel()

	newModel, _ := m.Update(keyMsg("G"))
	newModel, _ = newModel.(*model).Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m2 := newModel.(*model)
	if m2.view != viewStreams || m2.groupLevelSearch {
		t.Errorf("Esc on the unanswered prompt should back out of group search, got view=%v groupLevel=%v",
			m2.view, m2.groupLevelSearch)
	}
	if m2.eventsLoading {
		t.Error("backing out must not fire the unfiltered everything-query")
	}
}

func TestSplitPatterns(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		// A single pattern passes through untouched — CloudWatch pattern
		// syntax itself may contain spaces and special characters.
		{"ERROR", []string{"ERROR"}},
		{`{ $.level = "error" }`, []string{`{ $.level = "error" }`}},
		{"ERROR; timeout", []string{"ERROR", "timeout"}},
		{" ERROR ;; timeout ; ", []string{"ERROR", "timeout"}},
		{"", []string{""}},
		{" ; ; ", []string{""}},
	}
	for _, tt := range tests {
		got := SplitPatterns(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("SplitPatterns(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SplitPatterns(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
			}
		}
	}
}

func TestMergeDedupeSort(t *testing.T) {
	ev := func(id string, ts int64) types.FilteredLogEvent {
		return types.FilteredLogEvent{
			EventId:   aws.String(id),
			Timestamp: aws.Int64(ts),
			Message:   aws.String("m-" + id),
		}
	}
	// The same event matched by two patterns must appear once; the merged
	// timeline is time-ordered across batches.
	merged := mergeDedupeSort([][]types.FilteredLogEvent{
		{ev("a", 3000), ev("b", 1000)},
		{ev("a", 3000), ev("c", 2000)},
	}, 0)
	if len(merged) != 3 {
		t.Fatalf("expected 3 deduplicated events, got %d", len(merged))
	}
	for i, want := range []string{"b", "c", "a"} {
		if aws.ToString(merged[i].EventId) != want {
			t.Errorf("merged[%d] = %s, want %s", i, aws.ToString(merged[i].EventId), want)
		}
	}

	// The limit keeps the most recent events, not the first ones.
	limited := mergeDedupeSort([][]types.FilteredLogEvent{
		{ev("a", 1000), ev("b", 2000), ev("c", 3000)},
	}, 2)
	if len(limited) != 2 || aws.ToString(limited[0].EventId) != "b" {
		t.Errorf("limit should keep the most recent events, got %v", limited)
	}
}

func TestEventsMsgPartialFailureKeepsEvents(t *testing.T) {
	m := &model{}

	// Partial result: events shown, failure surfaced as a toast — never
	// silently, and never the full error screen.
	partial := eventsMsg{
		events: []types.FilteredLogEvent{{EventId: aws.String("e1"), Timestamp: aws.Int64(1000), Message: aws.String("hi")}},
		err:    testingError(`pattern "bad(": invalid`),
	}
	newModel, _ := m.Update(partial)
	m2 := newModel.(*model)
	if m2.err != nil {
		t.Error("a partial result must not raise the full error screen")
	}
	if len(m2.events) != 1 {
		t.Error("the successful patterns' events should be kept")
	}
	if !strings.Contains(m2.toast, "Some patterns failed") {
		t.Errorf("the failed pattern must be surfaced, got toast %q", m2.toast)
	}

	// Total failure still raises the error screen.
	newModel, _ = m2.Update(eventsMsg{err: testingError("denied")})
	if newModel.(*model).err == nil {
		t.Error("a fully failed query should raise the error screen")
	}
}

// Every pane/page carries a fixed name in its heading so it can be referred
// to unambiguously; renaming one is a breaking change to docs and help.
func TestPaneNamesAreStable(t *testing.T) {
	m := &model{
		width:  120,
		height: 40,
		groups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: "us-east-1"},
		},
		streams:      []types.LogStream{{LogStreamName: aws.String("s-1")}},
		regions:      []string{"us-east-1"},
		groupSearch:  textinput.New(),
		streamSearch: textinput.New(),
		eventSearch:  textinput.New(),
		lookback:     defaultLookback,
	}
	m.filterGroups()
	m.filterStreams()

	if out := m.renderSidebar(42); !strings.Contains(out, "[1] Log groups") {
		t.Error("sidebar should be named [1] Log groups")
	}
	if out := m.renderStreamsPanel(70); !strings.Contains(out, "[2] Log streams") {
		t.Error("streams panel should be named [2] Log streams")
	}
	if out := m.renderEventsPanel(70); !strings.Contains(out, "[3] Log events") {
		t.Error("events panel should be named [3] Log events")
	}

	m.viewer = logViewer{active: true, title: "s-1 [us-east-1]", seen: map[string]bool{}, wrapW: 80}
	if out := m.renderViewer(); !strings.Contains(out, "Log viewer —") {
		t.Error("the full-screen page should be named Log viewer")
	}
}

func TestClearAllFilters(t *testing.T) {
	m := &model{
		view:  viewEvents,
		focus: focusEvents,
		groups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/api")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/worker")}, Region: "us-east-1"},
		},
		streams: []types.LogStream{
			{LogStreamName: aws.String("2026/08/08/one")},
			{LogStreamName: aws.String("2026/08/08/two")},
		},
		groupSearch:  textinput.New(),
		streamSearch: textinput.New(),
		eventSearch:  textinput.New(),
		lookback:     defaultLookback,
	}
	m.groupSearch.SetValue("api")
	m.streamSearch.SetValue("two")
	m.eventSearch.SetValue("ERROR")
	m.filterGroups()
	m.filterStreams()
	// The narrowed selection is "/aws/lambda/api" / ".../two".
	if len(m.filteredGroups) != 1 || len(m.filteredStreams) != 1 {
		t.Fatalf("fixture should be narrowed, got %d groups %d streams", len(m.filteredGroups), len(m.filteredStreams))
	}

	newModel, cmd := m.Update(keyMsg("C"))
	m2 := newModel.(*model)
	if m2.groupSearch.Value() != "" || m2.streamSearch.Value() != "" || m2.eventSearch.Value() != "" {
		t.Error("C should clear all three filters")
	}
	if len(m2.filteredGroups) != 2 || len(m2.filteredStreams) != 2 {
		t.Errorf("lists should widen after clearing, got %d groups %d streams", len(m2.filteredGroups), len(m2.filteredStreams))
	}
	// The selection stays on the same group/stream, not the same index.
	if g, _ := m2.selectedGroup(); aws.ToString(g.LogGroupName) != "/aws/lambda/api" {
		t.Errorf("selection should stay on the previously selected group, got %q", aws.ToString(g.LogGroupName))
	}
	if got := aws.ToString(m2.filteredStreams[m2.selectedStreamIdx].LogStreamName); got != "2026/08/08/two" {
		t.Errorf("selection should stay on the previously selected stream, got %q", got)
	}
	if cmd == nil {
		t.Error("clearing the event pattern on the events view should re-run the query")
	}
	if !m2.eventsLoading {
		t.Error("the events panel should show loading while the query re-runs")
	}
}

func TestClearAllFiltersNoop(t *testing.T) {
	m := &model{
		groupSearch:  textinput.New(),
		streamSearch: textinput.New(),
		eventSearch:  textinput.New(),
	}
	newModel, _ := m.Update(keyMsg("C"))
	m2 := newModel.(*model)
	if m2.toast != "No filters active" {
		t.Errorf("C with nothing to clear should say so, got %q", m2.toast)
	}
	if m2.eventsLoading {
		t.Error("C with nothing to clear must not re-query")
	}
}

func TestCountLabel(t *testing.T) {
	if got := countLabel(5, 40, false); got != "5" {
		t.Errorf("unfiltered count = %q, want 5", got)
	}
	if got := countLabel(5, 40, true); got != "5/40" {
		t.Errorf("filtered count = %q, want 5/40", got)
	}
}

func TestAppliedFilterStaysVisible(t *testing.T) {
	m := &model{
		width:  100,
		height: 30,
		groups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/api")}, Region: "us-east-1"},
		},
		regions:      []string{"us-east-1"},
		groupSearch:  textinput.New(),
		streamSearch: textinput.New(),
	}
	m.groupSearch.SetValue("api")
	m.filterGroups()

	// The applied (inactive-input) filter must stay on screen — an invisible
	// filter reads as "my groups disappeared".
	out := m.renderSidebar(42)
	if !strings.Contains(out, "api") || !strings.Contains(out, "C clears") {
		t.Errorf("sidebar should show the applied filter and the clear hint:\n%s", out)
	}

	m2 := &model{width: 100, height: 30, streamSearch: textinput.New(), groupSearch: textinput.New()}
	m2.streamSearch.SetValue("stderr")
	out = m2.renderStreamsPanel(60)
	if !strings.Contains(out, "stderr") {
		t.Errorf("streams panel should show the applied filter:\n%s", out)
	}
}

func TestDownloadKeyStartsAndGuards(t *testing.T) {
	m := &model{
		focus: focusGroups,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("g1")}, Region: "us-east-1"},
		},
		eventSearch: textinput.New(),
		lookback:    defaultLookback,
	}

	keyD := tea.KeyPressMsg{Code: 'D', Text: string('D')}
	newModel, cmd := m.Update(keyD)
	m2 := newModel.(*model)
	if !m2.downloading {
		t.Error("expected D to start a download")
	}
	if cmd == nil {
		t.Error("expected D to return a command")
	}

	// A second D while one is running must not start another fetch.
	newModel, _ = m2.Update(keyD)
	m3 := newModel.(*model)
	if m3.toast != "A download is already running…" {
		t.Errorf("expected in-progress guard toast, got %q", m3.toast)
	}
}

func TestDownloadKeyNoGroupSelected(t *testing.T) {
	m := &model{focus: focusGroups, eventSearch: textinput.New(), lookback: defaultLookback}
	newModel, _ := m.Update(tea.KeyPressMsg{Code: 'D', Text: string('D')})
	if newModel.(*model).downloading {
		t.Error("D with no group selected must not start a download")
	}
}

func TestDownloadMsgOutcomes(t *testing.T) {
	m := &model{downloading: true}
	newModel, _ := m.Update(downloadMsg{path: "/tmp/x.log", count: 42})
	m2 := newModel.(*model)
	if m2.downloading {
		t.Error("downloadMsg should clear the downloading flag")
	}
	if !strings.Contains(m2.toast, "42") || !strings.Contains(m2.toast, "/tmp/x.log") {
		t.Errorf("success toast should carry count and path, got %q", m2.toast)
	}

	m2.downloading = true
	newModel, _ = m2.Update(downloadMsg{path: "/tmp/x.log", count: 50000, truncated: true})
	if got := newModel.(*model).toast; !strings.Contains(got, "truncated") {
		t.Errorf("truncated download must say so, got %q", got)
	}

	m3 := newModel.(*model)
	m3.downloading = true
	newModel, _ = m3.Update(downloadMsg{err: testingError("boom")})
	m4 := newModel.(*model)
	if m4.downloading {
		t.Error("downloadMsg error should clear the downloading flag")
	}
	if !strings.Contains(m4.toast, "boom") {
		t.Errorf("failure toast should carry the error, got %q", m4.toast)
	}
}

func TestFilterGroupsMatchesRegion(t *testing.T) {
	m := &model{
		groups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn1")}, Region: "us-east-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn1")}, Region: "eu-west-1"},
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn2")}, Region: "eu-west-1"},
		},
		groupSearch: textinput.New(),
	}

	m.groupSearch.SetValue("eu-west")
	m.filterGroups()
	if len(m.filteredGroups) != 2 {
		t.Errorf("expected 2 groups matching region filter, got %d", len(m.filteredGroups))
	}
}

func TestStreamsMsgDroppedForWrongRegion(t *testing.T) {
	// The same log group name exists in two regions; a streams response for
	// the non-selected region must be dropped.
	m := &model{
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn1")}, Region: "eu-west-1"},
		},
		streamsLoading: true,
	}

	stale := streamsMsg{
		groupName: "/aws/lambda/fn1",
		region:    "us-east-1",
		streams:   []types.LogStream{{LogStreamName: aws.String("stale")}},
	}
	newModel, _ := m.Update(stale)
	m2 := newModel.(*model)
	if !m2.streamsLoading || len(m2.streams) != 0 {
		t.Errorf("stale-region streams response should be dropped, got loading=%v streams=%d",
			m2.streamsLoading, len(m2.streams))
	}

	fresh := streamsMsg{
		groupName: "/aws/lambda/fn1",
		region:    "eu-west-1",
		streams:   []types.LogStream{{LogStreamName: aws.String("fresh")}},
	}
	newModel, _ = m2.Update(fresh)
	m3 := newModel.(*model)
	if m3.streamsLoading || len(m3.streams) != 1 {
		t.Errorf("matching-region streams response should apply, got loading=%v streams=%d",
			m3.streamsLoading, len(m3.streams))
	}
}
