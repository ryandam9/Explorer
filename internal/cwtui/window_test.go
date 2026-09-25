package cwtui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/textinput"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/charmbracelet/x/ansi"
)

func streamWithLastEvent(t time.Time) *types.LogStream {
	return &types.LogStream{LogStreamName: aws.String("s"), LastEventTimestamp: aws.Int64(t.UnixMilli())}
}

func TestWindowFor(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour
	fromNow := now.Add(-week).UnixMilli()

	// A group-wide query always counts back from now.
	if w := windowFor(week, now, nil); w.start != fromNow || w.anchored() {
		t.Errorf("group query: start=%d anchored=%v, want %d from now", w.start, w.anchored(), fromNow)
	}
	// A stream with events inside the window: unchanged.
	if w := windowFor(week, now, streamWithLastEvent(now.Add(-2*time.Hour))); w.start != fromNow || w.anchored() {
		t.Errorf("recent stream: start=%d anchored=%v, want %d from now", w.start, w.anchored(), fromNow)
	}
	// A stream with no events: unchanged (nothing to anchor to).
	if w := windowFor(week, now, &types.LogStream{LogStreamName: aws.String("s")}); w.start != fromNow || w.anchored() {
		t.Errorf("empty stream: start=%d anchored=%v, want %d from now", w.start, w.anchored(), fromNow)
	}
	// A stream whose last event is older than the window: the window moves
	// back to end at that event, so its events are found without widening it.
	last := time.Date(2026, 9, 3, 14, 22, 0, 0, time.UTC)
	w := windowFor(week, now, streamWithLastEvent(last))
	if !w.anchored() || w.anchor != last.UnixMilli() || w.start != last.Add(-week).UnixMilli() {
		t.Errorf("old stream: start=%d anchor=%d, want start %d anchor %d",
			w.start, w.anchor, last.Add(-week).UnixMilli(), last.UnixMilli())
	}
	if want := "7d before this stream's last event (" + formatMillis(last.UnixMilli()) + ")"; w.label() != want {
		t.Errorf("label = %q, want %q", w.label(), want)
	}
}

// The events pane for an old stream says which range it searched, and an
// empty result names that range instead of a bare "no events".
func TestEventsPaneShowsAnchoredWindow(t *testing.T) {
	last := time.Now().Add(-22 * 24 * time.Hour)
	m := &model{
		width: 140, height: 30, focus: focusEvents, view: viewEvents,
		lookback: 7 * 24 * time.Hour,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: "ap-southeast-2"},
		},
		filteredStreams: []types.LogStream{*streamWithLastEvent(last)},
		eventSearch:     textinput.New(),
	}
	out := ansi.Strip(m.renderEventsPanel(140))
	stamp := formatMillis(last.UnixMilli())
	if !strings.Contains(out, "Window: 7d before this stream's last event ("+stamp+")") {
		t.Errorf("header should name the anchored window, got:\n%s", out)
	}
	if !strings.Contains(out, "No matching log events in the 7d before this stream's last event") ||
		!strings.Contains(out, "to "+stamp+").") {
		t.Errorf("empty note should name the searched range, got:\n%s", out)
	}
}

// On a narrow panel the header falls back to the compact window form rather
// than overflowing the line.
func TestEventsPaneWindowLabelFitsNarrowPanel(t *testing.T) {
	last := time.Now().Add(-22 * 24 * time.Hour)
	m := &model{
		width: 100, height: 30, focus: focusEvents, view: viewEvents,
		lookback: 7 * 24 * time.Hour,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: "ap-southeast-2"},
		},
		filteredStreams: []types.LogStream{*streamWithLastEvent(last)},
		eventSearch:     textinput.New(),
	}
	const width = 90
	for _, line := range strings.Split(ansi.Strip(m.renderEventsPanel(width)), "\n") {
		if strings.Contains(line, "Window:") {
			if !strings.Contains(line, "Window: 7d to "+formatMillis(last.UnixMilli())+" (p)") {
				t.Errorf("narrow header should use the compact window form, got %q", line)
			}
			if ansi.StringWidth(line) > width+2 { // the panel's two border columns
				t.Errorf("header line is %d wide, over the %d-wide panel: %q", ansi.StringWidth(line), width, line)
			}
		}
	}
}

// fakeLogsClient is a CWLogsClient whose FilterLogEvents calls go to a local
// server that records each request's StartTime and returns no events.
func fakeLogsClient(t *testing.T, region string) (*CWLogsClient, func() []int64) {
	t.Helper()
	var mu sync.Mutex
	var starts []int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ StartTime int64 }
		_ = json.NewDecoder(r.Body).Decode(&in)
		mu.Lock()
		starts = append(starts, in.StartTime)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"events":[]}`))
	}))
	t.Cleanup(srv.Close)
	cl := cloudwatchlogs.New(cloudwatchlogs.Options{
		Region:       region,
		BaseEndpoint: aws.String(srv.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
	})
	return &CWLogsClient{clients: map[string]*cloudwatchlogs.Client{region: cl}, regions: []string{region}},
		func() []int64 { mu.Lock(); defer mu.Unlock(); return append([]int64(nil), starts...) }
}

// Opening an old stream queries FilterLogEvents from the anchored window, so
// its events are fetched without the user widening the window.
func TestLoadEventsQueriesAnchoredWindow(t *testing.T) {
	const region = "ap-southeast-2"
	client, starts := fakeLogsClient(t, region)
	last := time.Now().Add(-22 * 24 * time.Hour)
	lookback := 7 * 24 * time.Hour
	m := &model{
		ctx: context.Background(), client: client, lookback: lookback,
		filteredGroups: []LogGroup{
			{LogGroup: types.LogGroup{LogGroupName: aws.String("/aws/lambda/fn")}, Region: region},
		},
		filteredStreams: []types.LogStream{*streamWithLastEvent(last)},
		eventSearch:     textinput.New(),
	}
	msg := m.loadEventsCmd()()
	if em, ok := msg.(eventsMsg); !ok || em.err != nil {
		t.Fatalf("loadEventsCmd returned %#v", msg)
	}
	got := starts()
	want := last.UnixMilli() - lookback.Milliseconds()
	if len(got) != 1 || got[0] != want {
		t.Errorf("FilterLogEvents StartTime = %v, want [%d] (7d before the stream's last event)", got, want)
	}
}
