package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/ui"
)

// newTestModel builds a model, sizes it, and feeds it a chunk of fake
// resources, mimicking the startup sequence without touching AWS.
func newTestModel(t *testing.T, width, height int) tuiModel {
	t.Helper()
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)

	resources := []model.Resource{
		{Service: "s3", Type: "bucket", Region: "us-east-1", ID: "bkt-1", Name: "logs", State: "active"},
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-abc123", Name: "web-1", State: "running"},
		{Service: "ec2", Type: "instance", Region: "eu-west-1", ID: "i-def456", Name: "web-2", State: "stopped"},
	}

	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = update(m, chunkMsg(model.ResultChunk{Resources: resources}))
	return m
}

func update(m tuiModel, msg tea.Msg) tuiModel {
	next, _ := m.Update(msg)
	return next.(tuiModel)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestViewShowsResourcesAndContextHints(t *testing.T) {
	m := newTestModel(t, 140, 40)
	plain := ansi.Strip(m.View())

	for _, want := range []string{"i-abc123", "web-1", "SERVICES", "ec2", "s3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q", want)
		}
	}
	// Table focus: the status bar advertises table shortcuts.
	for _, want := range []string{"navigate", "detail", "filter", "help"} {
		if !strings.Contains(plain, want) {
			t.Errorf("status bar missing table-context hint %q", want)
		}
	}
}

func TestDetailOverlayChangesHints(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, key("enter")) // open detail for the selected row
	if !m.showDetail {
		t.Fatal("enter on table should open the detail panel")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "close") {
		t.Errorf("detail-focus status bar should offer Esc close, got %q", lastLine(plain))
	}
	m = update(m, key("esc"))
	if m.showDetail {
		t.Error("esc should close the detail panel")
	}
}

func TestQuickTextFilter(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, key("/"))
	if !m.filtering {
		t.Fatal("/ should enter filter mode")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "Enter keep filter") {
		t.Errorf("filter-mode hints not shown: %q", lastLine(plain))
	}

	m = update(m, key("web-2"))
	rows := m.allRows[m.currentService()]
	if len(rows) != 1 {
		t.Fatalf("filter 'web-2' should leave 1 row, got %d", len(rows))
	}
	if res, ok := m.selectedResource(); !ok || res.ID != "i-def456" {
		t.Errorf("selected resource = %+v, want i-def456", res)
	}

	// Esc clears the filter entirely.
	m = update(m, key("esc"))
	if m.filterText != "" || len(m.allRows[m.currentService()]) != 3 {
		t.Errorf("esc should clear the filter, got text=%q rows=%d",
			m.filterText, len(m.allRows[m.currentService()]))
	}
}

func TestNarrowTerminalScrollsColumns(t *testing.T) {
	m := newTestModel(t, 70, 30)
	l, r := m.table.ColScrollInfo()
	if l+r == 0 {
		t.Fatal("narrow terminal should hide columns and enable scrolling")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "more cols") {
		t.Error("scroll indicator missing from narrow view")
	}
	if !hasHint(m.statusHints(), "</>") {
		t.Error("status hints should advertise </> column scrolling when columns are hidden")
	}

	m = update(m, key(">"))
	if l2, _ := m.table.ColScrollInfo(); l2 != 1 {
		t.Errorf("> should scroll one column right, hiddenLeft=%d", l2)
	}
}

func TestResetHintOnlyWithActiveFilter(t *testing.T) {
	m := newTestModel(t, 140, 40)
	if hasHint(m.statusHints(), "r") {
		t.Error("reset hint shown with no active filter")
	}
	m.filterText = "web"
	if !hasHint(m.statusHints(), "r") {
		t.Error("reset hint missing while a filter is active")
	}
}

func TestErrorsOverlay(t *testing.T) {
	m := newTestModel(t, 140, 40)

	// With no errors, 'e' is a no-op and no errors hint is offered.
	if hasHint(m.statusHints(), "e") {
		t.Error("errors hint shown with no errors collected")
	}
	m = update(m, key("e"))
	if m.showErrors {
		t.Fatal("'e' should do nothing when there are no errors")
	}

	// Feed an access-denied error, then open the overlay.
	m = update(m, chunkMsg(model.ResultChunk{Errors: []model.ExploreError{{
		Service: "rds", Region: "us-east-1", Code: "AccessDenied",
		Message: "Insufficient privileges — required IAM permission: rds:DescribeDBInstances",
	}}}))
	if !hasHint(m.statusHints(), "e") {
		t.Error("errors hint missing once an error was collected")
	}

	m = update(m, key("e"))
	if !m.showErrors {
		t.Fatal("'e' should open the errors overlay when errors exist")
	}
	plain := ansi.Strip(m.View())
	for _, want := range []string{"INSUFFICIENT PRIVILEGES", "RDS", "rds:DescribeDBInstances"} {
		if !strings.Contains(plain, want) {
			t.Errorf("errors overlay missing %q", want)
		}
	}

	// Esc closes it again.
	m = update(m, key("esc"))
	if m.showErrors {
		t.Error("esc should close the errors overlay")
	}
}

func hasHint(hints []ui.KeyHint, key string) bool {
	for _, h := range hints {
		if h.Key == key {
			return true
		}
	}
	return false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
