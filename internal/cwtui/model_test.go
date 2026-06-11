package cwtui

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
		filteredGroups: []types.LogGroup{
			{LogGroupName: aws.String("group1")},
			{LogGroupName: aws.String("group2")},
			{LogGroupName: aws.String("group3")},
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
		groups: []types.LogGroup{
			{LogGroupName: aws.String("/aws/lambda/fn1")},
			{LogGroupName: aws.String("/aws/lambda/fn2")},
			{LogGroupName: aws.String("/aws/ecs/service")},
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
		filteredGroups: []types.LogGroup{
			{LogGroupName: aws.String("g1")},
		},
		groupSearch: textinput.New(),
	}

	// Test basic key handlers
	newModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m2 := newModel.(*model)
	if m2.focus != focusStreams {
		t.Errorf("expected focus to change to focusStreams, got %v", m2.focus)
	}

	// Escape with watchMode active should deactivate it
	m2.watchMode = true
	newModel, cmd = m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	groups := []types.LogGroup{{LogGroupName: aws.String("g1")}}
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
