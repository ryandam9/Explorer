package logstui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/user/aws_explorer/internal/logs"
)

type fakeAPI struct{}

func (fakeAPI) DescribeLogGroups(context.Context, *cloudwatchlogs.DescribeLogGroupsInput,
	...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	return &cloudwatchlogs.DescribeLogGroupsOutput{}, nil
}

func (fakeAPI) FilterLogEvents(context.Context, *cloudwatchlogs.FilterLogEventsInput,
	...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	return &cloudwatchlogs.FilterLogEventsOutput{}, nil
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(context.Background(), fakeAPI{}, "us-east-1", "spotted-pardalote", "", 15*time.Minute, "")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return mm.(Model)
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	mm, _ := m.Update(msg)
	return mm.(Model)
}

func TestClosestWindow(t *testing.T) {
	cases := map[time.Duration]time.Duration{
		0:                   15 * time.Minute,
		15 * time.Minute:    15 * time.Minute,
		45 * time.Minute:    time.Hour,
		2 * time.Hour:       3 * time.Hour,
		48 * time.Hour:      3 * 24 * time.Hour,
		30 * 24 * time.Hour: 7 * 24 * time.Hour,
	}
	for in, want := range cases {
		if got := timeWindows[closestWindow(in)]; got != want {
			t.Errorf("closestWindow(%v) → %v, want %v", in, got, want)
		}
	}
}

func TestFormatWindow(t *testing.T) {
	cases := map[time.Duration]string{
		15 * time.Minute:   "15m",
		time.Hour:          "1h",
		3 * 24 * time.Hour: "3d",
	}
	for in, want := range cases {
		if got := formatWindow(in); got != want {
			t.Errorf("formatWindow(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("abcdefghij", 4)
	if len(got) != 3 || got[0] != "abcd" || got[2] != "ij" {
		t.Errorf("wrapText = %v", got)
	}
	got = wrapText("a\nb", 10)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("wrapText with newline = %v", got)
	}
	if got := wrapText("", 10); len(got) != 1 {
		t.Errorf("wrapText empty = %v", got)
	}
}

func TestClipString(t *testing.T) {
	if got := clipString("hello", 10); got != "hello" {
		t.Errorf("clipString short = %q", got)
	}
	if got := clipString("hello world", 6); got != "hello…" {
		t.Errorf("clipString long = %q", got)
	}
}

func TestGroupsMsg_PopulatesTableAndFlagsPartial(t *testing.T) {
	m := newTestModel(t)
	m = update(t, m, groupsMsg{
		groups: []logs.Group{{Name: "/aws/lambda/a"}, {Name: "/ecs/web"}},
		err:    errors.New("throttled"),
	})

	if len(m.groupsTable.Rows()) != 2 {
		t.Fatalf("expected 2 group rows, got %d", len(m.groupsTable.Rows()))
	}
	if !strings.Contains(m.errMsg, "partial group list kept") {
		t.Errorf("expected partial marker in error, got %q", m.errMsg)
	}
}

func TestGroupSearch_FiltersRows(t *testing.T) {
	m := newTestModel(t)
	m = update(t, m, groupsMsg{groups: []logs.Group{{Name: "/aws/lambda/a"}, {Name: "/ecs/web"}}})

	m.groupSearch.SetValue("lambda")
	m.refreshGroupRows()
	rows := m.groupsTable.Rows()
	if len(rows) != 1 || rows[0][0] != "/aws/lambda/a" {
		t.Errorf("expected only the lambda group, got %v", rows)
	}
}

func TestEventsMsg_LoadsAndAppends(t *testing.T) {
	m := newTestModel(t)
	m.currentGroup = "g"
	token := "next"

	m = update(t, m, eventsMsg{
		group: "g",
		page: logs.Page{
			Events:    []logs.Event{{Message: "one", Timestamp: time.Now()}},
			NextToken: &token,
		},
	})
	if len(m.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(m.events))
	}
	if m.nextToken == nil {
		t.Fatal("expected nextToken to be retained")
	}
	if !strings.Contains(m.statusMsg, "more available") {
		t.Errorf("expected more-available hint, got %q", m.statusMsg)
	}

	m = update(t, m, eventsMsg{
		group:  "g",
		append: true,
		page:   logs.Page{Events: []logs.Event{{Message: "two", Timestamp: time.Now()}}},
	})
	if len(m.events) != 2 {
		t.Fatalf("expected 2 events after append, got %d", len(m.events))
	}
	if m.nextToken != nil {
		t.Error("expected nextToken cleared when the window is exhausted")
	}
}

func TestEventsMsg_StaleGroupIgnored(t *testing.T) {
	m := newTestModel(t)
	m.currentGroup = "current"
	m = update(t, m, eventsMsg{
		group: "previous",
		page:  logs.Page{Events: []logs.Event{{Message: "stale"}}},
	})
	if len(m.events) != 0 {
		t.Error("expected events from a stale group selection to be dropped")
	}
}

func TestKeyT_CyclesWindow(t *testing.T) {
	m := newTestModel(t)
	if m.window() != 15*time.Minute {
		t.Fatalf("initial window = %v", m.window())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if m.window() != time.Hour {
		t.Errorf("window after t = %v, want 1h", m.window())
	}
}

func TestRenderEvents_WrapsLongMessages(t *testing.T) {
	m := newTestModel(t)
	m.currentGroup = "g"
	m.viewport.Width = 60
	m.events = []logs.Event{{
		Timestamp: time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC),
		Stream:    "s",
		Message:   strings.Repeat("x", 200),
	}}
	out := m.renderEvents()
	if !strings.Contains(out, "06-11 10:00:00") {
		t.Errorf("expected formatted timestamp in output")
	}
	if lines := strings.Count(out, "\n"); lines < 2 {
		t.Errorf("expected the long message to wrap onto multiple lines, got %d", lines)
	}
}
