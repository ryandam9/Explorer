package trailtui

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/trail"
)

func testEvents() []trail.Event {
	base := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	return []trail.Event{
		{Time: base, EventName: "RunInstances", EventSource: "ec2.amazonaws.com", Region: "us-east-1",
			Principal: "user/alice", SourceIP: "198.51.100.2", UserAgent: "aws-cli/2.15.0",
			AccessKeyID: "AKIAEXAMPLE", EventID: "evt-1", MFA: true,
			ErrorCode: "Client.UnauthorizedOperation", ErrorMessage: "You are not authorized to perform this operation",
			Resources: []trail.Resource{{Type: "AWS::EC2::Instance", Name: "i-0abc"}}},
		{Time: base.Add(-time.Hour), EventName: "AuthorizeSecurityGroupIngress", EventSource: "ec2.amazonaws.com",
			Region: "us-east-1", Principal: "role/deploy", SourceIP: "203.0.113.7", FromConsole: true},
		{Time: base.Add(-2 * time.Hour), EventName: "DeleteBucket", EventSource: "s3.amazonaws.com",
			Region: "eu-west-1", Principal: "root", SourceIP: "192.0.2.1"},
	}
}

// newTestModel builds a sized model with the sample events streamed in.
func newTestModel(t *testing.T) Model {
	t.Helper()
	m := newSizedModel([]string{"us-east-1"}, trail.Options{})
	return streamRegion(m, "us-east-1", testEvents())
}

// newSizedModel builds a model and applies a window size, ready for messages.
func newSizedModel(regions []string, opts trail.Options) Model {
	m := New(context.Background(), aws.Config{}, regions, trail.Filter{}, opts, "account-wide activity")
	// Wide enough that every column is on-screen (no horizontal scroll) so
	// table-content assertions are stable.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 220, Height: 40})
	return mm.(Model)
}

// streamRegion delivers one region's events and then closes the stream, the
// way Init's per-region goroutines would.
func streamRegion(m Model, region string, events []trail.Event) Model {
	m = update(m, regionMsg{region: region, events: events})
	return update(m, streamDoneMsg{})
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestLoadPopulatesTable(t *testing.T) {
	m := newTestModel(t)
	if m.loading {
		t.Error("loading should be false after the stream completes")
	}
	if len(m.all) != 3 || len(m.visible) != 3 {
		t.Fatalf("all=%d visible=%d, want 3/3", len(m.all), len(m.visible))
	}
	out := m.View()
	for _, want := range []string{"RunInstances", "DeleteBucket", "user/alice"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestErrorsOnlyToggle(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("x"))
	if !m.errorsOnly {
		t.Fatal("x should enable failed-only")
	}
	if len(m.visible) != 1 || m.visible[0].EventName != "RunInstances" {
		t.Errorf("failed-only should leave just the denied call, got %d: %+v", len(m.visible), m.visible)
	}
	m = update(m, key("x"))
	if m.errorsOnly || len(m.visible) != 3 {
		t.Errorf("x again should restore all events, got %d", len(m.visible))
	}
}

func TestFilterNarrows(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("/"))
	if !m.filtering {
		t.Fatal("/ should start filtering")
	}
	for _, r := range "delete" {
		m = update(m, key(string(r)))
	}
	if len(m.visible) != 1 || m.visible[0].EventName != "DeleteBucket" {
		t.Errorf("filter 'delete' should match one event, got %d: %+v", len(m.visible), m.visible)
	}
}

func TestResetClearsFilterSortAndToggle(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("x")) // failed only
	m = update(m, key("s")) // sort by TIME asc
	m = update(m, key("r")) // reset
	if m.errorsOnly || m.sortCol != -1 || m.filterIn.Value() != "" {
		t.Errorf("reset failed: errorsOnly=%v sortCol=%d filter=%q", m.errorsOnly, m.sortCol, m.filterIn.Value())
	}
	if len(m.visible) != 3 {
		t.Errorf("reset should show all events, got %d", len(m.visible))
	}
}

func TestLoadErrorShownInBody(t *testing.T) {
	// A single-region feed whose only region fails surfaces the error.
	m := newSizedModel([]string{"us-east-1"}, trail.Options{})
	m = update(m, regionMsg{region: "us-east-1", err: errTest("not authorized")})
	m = update(m, streamDoneMsg{})
	if m.loadErr == nil {
		t.Fatal("an all-regions-failed stream should set loadErr")
	}
	if !strings.Contains(m.View(), "not authorized") {
		t.Errorf("load error should surface in the body:\n%s", m.View())
	}
}

func TestStreamMergesRegionsNewestFirst(t *testing.T) {
	base := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	m := newSizedModel([]string{"us-east-1", "eu-west-1"}, trail.Options{})
	// Regions arrive out of order; the merged feed must still be newest-first.
	m = update(m, regionMsg{region: "eu-west-1", events: []trail.Event{
		{Time: base.Add(-3 * time.Hour), EventName: "OldEvent"},
	}})
	if m.loading != true {
		t.Error("feed should stay loading until every region reports")
	}
	if len(m.visible) != 1 || m.visible[0].EventName != "OldEvent" {
		t.Fatalf("first region should be visible immediately, got %+v", m.visible)
	}
	m = update(m, regionMsg{region: "us-east-1", events: []trail.Event{
		{Time: base, EventName: "NewEvent"},
	}})
	m = update(m, streamDoneMsg{})
	if m.loading {
		t.Error("feed should stop loading once the stream is done")
	}
	if len(m.visible) != 2 || m.visible[0].EventName != "NewEvent" {
		t.Fatalf("merged feed should be newest-first, got %+v", m.visible)
	}
}

func TestPartialRegionFailureKeepsResults(t *testing.T) {
	m := newSizedModel([]string{"us-east-1", "eu-west-1"}, trail.Options{})
	m = update(m, regionMsg{region: "us-east-1", events: testEvents()})
	m = update(m, regionMsg{region: "eu-west-1", err: errTest("throttled")})
	m = update(m, streamDoneMsg{})
	if m.loadErr != nil {
		t.Errorf("a partial failure must not blank the feed, got loadErr=%v", m.loadErr)
	}
	if len(m.visible) != 3 {
		t.Errorf("the surviving region's events should remain, got %d", len(m.visible))
	}
	if !strings.Contains(m.View(), "1 region(s) failed") {
		t.Errorf("a partial failure should be noted in the header:\n%s", m.View())
	}
}

func TestLimitCapsToNewest(t *testing.T) {
	base := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	evs := make([]trail.Event, 5)
	for i := range evs {
		evs[i] = trail.Event{Time: base.Add(-time.Duration(i) * time.Hour), EventName: "Event" + strconv.Itoa(i)}
	}
	m := newSizedModel([]string{"us-east-1"}, trail.Options{Limit: 3})
	m = streamRegion(m, "us-east-1", evs)
	if m.limit != 3 {
		t.Fatalf("limit = %d, want 3", m.limit)
	}
	if !m.capped || len(m.visible) != 3 {
		t.Fatalf("feed should cap to the newest 3, got capped=%v visible=%d", m.capped, len(m.visible))
	}
	if m.visible[0].EventName != "Event0" || m.visible[2].EventName != "Event2" {
		t.Errorf("cap should keep the newest events, got %+v", m.visible)
	}
}

func TestDetailOverlayOpens(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("enter"))
	if m.overlay != overlayDetail {
		t.Fatal("enter should open the detail overlay")
	}
	if !strings.Contains(m.View(), "Source IP") {
		t.Errorf("detail overlay should show event fields:\n%s", m.View())
	}
	m = update(m, key("esc"))
	if m.overlay != overlayNone {
		t.Error("esc should close the overlay")
	}
}

func TestServiceAndRegionColumns(t *testing.T) {
	out := newTestModel(t).View()
	for _, want := range []string{"SERVICE", "REGION", "ec2", "us-east-1", "eu-west-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table view missing %q:\n%s", want, out)
		}
	}
}

func TestDetailOverlayShowsRichFields(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("enter")) // newest event (RunInstances) is selected
	out := m.View()
	// The denied RunInstances event carries the full set of attributes.
	for _, want := range []string{
		"User agent", "aws-cli/2.15.0", "MFA", "Access key", "AKIAEXAMPLE",
		"Error", "not authorized", "Resources", "i-0abc", "Event ID", "evt-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail overlay missing %q:\n%s", want, out)
		}
	}
}

func TestDetailTextIsPlainAndSelfContained(t *testing.T) {
	ev := testEvents()[0] // denied RunInstances with the full field set
	out := detailText(&ev)

	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("detail text should carry no ANSI escapes:\n%q", out)
	}
	// The copied panel should stand alone: event name plus every recorded field,
	// and none of the surrounding table or overlay chrome.
	for _, want := range []string{
		"RunInstances", "Time:", "Principal:", "Source IP:", "198.51.100.2",
		"User agent:", "aws-cli/2.15.0", "Access key:", "AKIAEXAMPLE",
		"Outcome:", "✗ Client.UnauthorizedOperation", "Error:", "Event ID:", "evt-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail text missing %q:\n%s", want, out)
		}
	}
	// Optional fields CloudTrail didn't record are dropped entirely.
	ev2 := testEvents()[2] // DeleteBucket: no user agent, error, or resources
	out2 := detailText(&ev2)
	for _, absent := range []string{"User agent:", "Error:", "Resources:", "Event ID:"} {
		if strings.Contains(out2, absent) {
			t.Errorf("detail text should omit empty optional field %q:\n%s", absent, out2)
		}
	}
}

// captureClipboard redirects the clipboard sink to a buffer for the duration of
// a test, since the real clipboard is unavailable in headless CI.
func captureClipboard(t *testing.T) *string {
	t.Helper()
	var got string
	prev := clipboardWrite
	clipboardWrite = func(s string) error { got = s; return nil }
	t.Cleanup(func() { clipboardWrite = prev })
	return &got
}

func TestCopyDetailOverlayCopiesPanel(t *testing.T) {
	got := captureClipboard(t)
	m := newTestModel(t)
	m = update(m, key("enter")) // open detail for the newest event
	m = update(m, key("y"))

	if !strings.Contains(m.status, "RunInstances details") {
		t.Errorf("copying with the overlay open should report the detail copy, got %q", m.status)
	}
	// The whole panel is copied — multiple labelled fields — not just the name,
	// and none of the table behind the overlay comes along.
	for _, want := range []string{"RunInstances", "Source IP:", "198.51.100.2", "Event ID:"} {
		if !strings.Contains(*got, want) {
			t.Errorf("clipboard missing %q:\n%s", want, *got)
		}
	}
	if strings.Contains(*got, "DeleteBucket") {
		t.Errorf("clipboard should hold only the overlay, not other table rows:\n%s", *got)
	}
}

func TestCopyEventNameWithoutOverlay(t *testing.T) {
	got := captureClipboard(t)
	m := newTestModel(t)
	m = update(m, key("y"))

	if m.status != "copied RunInstances" {
		t.Errorf("copying without the overlay should copy the event name, got %q", m.status)
	}
	if *got != "RunInstances" {
		t.Errorf("clipboard should hold just the event name, got %q", *got)
	}
}

func TestFilterMatchesServiceAndUserAgent(t *testing.T) {
	m := newTestModel(t)
	m = update(m, key("/"))
	for _, r := range "aws-cli" {
		m = update(m, key(string(r)))
	}
	if len(m.visible) != 1 || m.visible[0].EventName != "RunInstances" {
		t.Errorf("filter on user agent should match the one event, got %d: %+v", len(m.visible), m.visible)
	}
}

func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestSortEventsByTime(t *testing.T) {
	evs := testEvents()
	sortEvents(evs, 1, true) // TIME ascending
	if !evs[0].Time.Before(evs[1].Time) || !evs[1].Time.Before(evs[2].Time) {
		t.Errorf("events not sorted ascending by time: %+v", evs)
	}
}

func TestOutcomeLabel(t *testing.T) {
	if got := outcomeLabel(trail.Event{}); got != "ok" {
		t.Errorf("successful outcome = %q, want ok", got)
	}
	if got := outcomeLabel(trail.Event{ErrorCode: "AccessDenied"}); got != "✗ AccessDenied" {
		t.Errorf("failed outcome = %q", got)
	}
}

func TestCountFailed(t *testing.T) {
	if got := countFailed(testEvents()); got != 1 {
		t.Errorf("countFailed = %d, want 1", got)
	}
}
