package lambdatui

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/table"
)

func newActivityTestModel(logs *stubLogs) *m {
	mm := &m{
		ctx: context.Background(),
		client: &Client{
			regions: []string{"us-east-1"},
			metrics: map[string]metricsAPI{"us-east-1": &stubMetrics{out: &cloudwatch.GetMetricDataOutput{}}},
			logs:    map[string]logsAPI{"us-east-1": logs},
		},
		regions: []string{"us-east-1"},
		width:   120,
		height:  32,
		tbl:     newLambdaTable(tabColumns(tabFunctions, false)),
		act:     activityState{inputs: newActivityInputs()},
		sortCol: -1,
	}
	mm.inv = Inventory{Functions: []Function{{Name: "copy-object", Region: "us-east-1", LogGroup: "/aws/lambda/copy-object"}}}
	mm.rebuild()
	return mm
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// runCmd executes a command (and every command in a batch), feeding the
// resulting messages back into the model — one synchronous step of the loop.
func runCmd(mm *m, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(mm, c)
		}
	case activityPageMsg, activityMetricsMsg, invocationMsg:
		_, next := mm.Update(msg)
		runCmd(mm, next)
	}
}

func TestActivityFlow(t *testing.T) {
	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{logEvt(1790000000000, "s", "Copied in/a.csv to out/a.csv")}, NextToken: aws.String("t1")},
		{Events: []cwltypes.FilteredLogEvent{logEvt(1790000001000, "s", "Copied in/b.csv to out/b.csv")}},
	}}
	mm := newActivityTestModel(logs)

	mm.Update(key("a"))
	if !mm.act.formActive || mm.act.fn.Name != "copy-object" {
		t.Fatalf("a should open the activity form for the selected function: %+v", mm.act.fn)
	}
	mm.act.inputs[actFieldDate].SetValue("yesterday")
	mm.Update(key("tab"))
	mm.Update(key(`Copied (\S+) to (\S+)`))
	_, cmd := mm.Update(key("enter"))
	if mm.act.formActive || !mm.act.active || !mm.act.scanning {
		t.Fatalf("enter should start the run: form=%v active=%v scanning=%v err=%q", mm.act.formActive, mm.act.active, mm.act.scanning, mm.act.formErr)
	}
	runCmd(mm, cmd)

	if mm.act.scanning || mm.act.scan == nil || len(mm.act.scan.Matches) != 2 || !mm.act.scan.StartsKnown() {
		t.Fatalf("the pull loop should read both pages: %+v", mm.act.scan)
	}
	if len(logs.calls) != 2 || aws.ToString(logs.calls[1].NextToken) != "t1" {
		t.Errorf("second page should use the first page's token: %d calls", len(logs.calls))
	}
	if mm.act.metricsLoading || mm.act.statsErr != nil {
		t.Errorf("metrics should have landed: loading=%v err=%v", mm.act.metricsLoading, mm.act.statsErr)
	}
	if got := len(mm.act.tbl.Rows()); got != 2 {
		t.Errorf("table rows = %d, want 2", got)
	}
	if cols := mm.act.tbl.Columns(); cols[3].Title != "$1" || cols[4].Title != "$2" {
		t.Errorf("capture groups should be columns: %+v", cols)
	}

	// Layout: the frame fills the terminal exactly, status bar last.
	view := mm.View()
	if h := lipgloss.Height(view); h != mm.height {
		t.Errorf("view height = %d, want %d", h, mm.height)
	}
	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[len(lines)-1], "2 matches") {
		t.Errorf("status bar should summarise the run, got %q", lines[len(lines)-1])
	}
	if !strings.Contains(view, "in/a.csv") || !strings.Contains(view, "0 invocations") {
		t.Errorf("view missing content:\n%s", view)
	}

	// A late message from a superseded run is ignored.
	before := len(mm.act.scan.Matches)
	mm.Update(activityPageMsg{gen: mm.act.gen - 1, events: []LogEvent{{Message: "Copied x to y"}}})
	if len(mm.act.scan.Matches) != before {
		t.Error("stale page adopted")
	}

	mm.Update(key("esc"))
	if mm.act.active {
		t.Error("esc should close the finished report")
	}
}

func TestActivityEscStopsScanFirst(t *testing.T) {
	mm := newActivityTestModel(&stubLogs{})
	mm.Update(key("a"))
	mm.act.inputs[actFieldPattern].SetValue("x")
	mm.Update(key("enter"))
	if !mm.act.scanning {
		t.Fatal("scan should be running")
	}
	cancelled := mm.act.ctx
	mm.Update(key("esc"))
	if mm.act.scanning || !mm.act.active || cancelled.Err() == nil {
		t.Fatalf("first esc stops the scan and keeps the report: scanning=%v active=%v", mm.act.scanning, mm.act.active)
	}
	if !strings.Contains(mm.act.scan.ScanNote(), "cancelled") {
		t.Errorf("a stopped scan must say so, note=%q", mm.act.scan.ScanNote())
	}
	mm.Update(key("esc"))
	if mm.act.active {
		t.Error("second esc closes the report")
	}
}

func TestActivityFormValidation(t *testing.T) {
	mm := newActivityTestModel(&stubLogs{})
	mm.Update(key("a"))
	mm.act.inputs[actFieldPattern].SetValue("(unclosed")
	mm.Update(key("enter"))
	if !mm.act.formActive || !strings.Contains(mm.act.formErr, "invalid regex") {
		t.Errorf("a bad regex keeps the form open with an error: %q", mm.act.formErr)
	}
}

// An empty regex lists every event of the day: all lines become rows, there is
// no MATCH column (nothing was searched for), and the header says so.
func TestActivityEmptyRegexListsAllEvents(t *testing.T) {
	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{
			logEvt(1790000000000, "s", "START RequestId: 3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c Version: $LATEST"),
			logEvt(1790000000100, "s", "[INFO]\t2026-09-23T21:35:37.940Z\t3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c\tSent: stocks: asx.db"),
			logEvt(1790000000200, "s", "END RequestId: 3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c"),
		}},
	}}
	mm := newActivityTestModel(logs)
	mm.Update(key("a"))
	mm.act.inputs[actFieldPattern].SetValue("")
	_, cmd := mm.Update(key("enter"))
	if mm.act.formActive || mm.act.scan == nil || !mm.act.scanning {
		t.Fatalf("an empty regex should start a scan: form=%v err=%q", mm.act.formActive, mm.act.formErr)
	}
	runCmd(mm, cmd)

	if got := len(mm.act.scan.Matches); got != 3 {
		t.Errorf("every event should be a row: %d, want 3", got)
	}
	for _, c := range mm.act.tbl.Columns() {
		if c.Title == "MATCH" {
			t.Error("listing all events has no matched text — no MATCH column")
		}
	}
	view := mm.View()
	if !strings.Contains(view, "Log scan all events  3 events read · 1 START lines\n") || !strings.Contains(view, "·  3 events") {
		t.Errorf("header should say all events, not a match count:\n%s", view)
	}

	// With a server filter, an empty regex keeps every filtered event.
	mm.Update(key("e"))
	mm.act.inputs[actFieldFilter].SetValue(`"Sent"`)
	mm.Update(key("enter"))
	if mm.act.formActive || mm.act.query.Filter != `"Sent"` || !matchesAll(mm.act.query.Pattern) {
		t.Errorf("a filter without a regex should run: form=%v err=%q", mm.act.formActive, mm.act.formErr)
	}
}

func TestActivityKeyOnlyOnFunctions(t *testing.T) {
	mm := newActivityTestModel(&stubLogs{})
	mm.switchTab(true) // Layers
	mm.Update(key("a"))
	if mm.act.formActive {
		t.Error("a is a function action; it must not open on the Layers tab")
	}
}

// A long message wraps onto continuation rows instead of being cut with "…":
// the MESSAGE column fills the panel, every word of the line is on screen, no
// line overruns the terminal, and the match stays one unit for the cursor.
func TestActivityMessageWraps(t *testing.T) {
	long := "REPORT RequestId: e96b3ee9-d986-4b29-b79d-742d17451c69 " + strings.Repeat("Duration: 1981.13 ms ", 20)
	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{
			logEvt(1790000000000, "s", long),
			logEvt(1790000001000, "s", "Duration: short"),
		}},
	}}
	mm := newActivityTestModel(logs)
	mm.width = 160
	mm.Update(key("a"))
	mm.Update(key("tab"))
	mm.Update(key("Duration"))
	_, cmd := mm.Update(key("enter"))
	runCmd(mm, cmd)

	// The long match spans several rows, the short one a single row.
	rows := mm.act.tbl.Rows()
	msg := func(r table.Row) string { return r[len(r)-1] }
	var wrapped []string
	for i := 0; i < len(rows)-1; i++ {
		if i > 0 && rows[i][0] != "" {
			t.Fatalf("continuation row %d should leave the fixed cells blank: %q", i, rows[i])
		}
		wrapped = append(wrapped, msg(rows[i]))
	}
	if len(wrapped) < 2 || msg(rows[len(rows)-1]) != "Duration: short" {
		t.Fatalf("the long message should wrap over several rows, then the short one: %q", rows)
	}
	if got := strings.Join(wrapped, " "); strings.Contains(got, "…") || strings.Count(got, "Duration: 1981.13 ms") != 20 {
		t.Errorf("wrapping should keep the whole line, got %q", got)
	}
	for _, line := range strings.Split(mm.View(), "\n") {
		if w := ansi.StringWidth(line); w > mm.width {
			t.Fatalf("line overruns the %d-col terminal (%d): %q", mm.width, w, line)
		}
	}

	// ↓ steps from one match to the next, not onto a wrapped line.
	if m, _ := mm.selectedMatch(); !strings.HasPrefix(m.Body, "REPORT") {
		t.Fatalf("first match should be selected, got %q", m.Body)
	}
	mm.Update(key("j"))
	if m, _ := mm.selectedMatch(); m.Body != "Duration: short" {
		t.Errorf("one step down should reach the second match, got %q", m.Body)
	}

	// A resize re-wraps (and keeps the selection): wider terminal, fewer lines.
	mm.Update(tea.WindowSizeMsg{Width: 400, Height: mm.height})
	if got := len(mm.act.tbl.Rows()); got >= len(rows) {
		t.Errorf("resize to 400 cols should need fewer rows: %d -> %d", len(rows), got)
	}
	if m, _ := mm.selectedMatch(); m.Body != "Duration: short" {
		t.Errorf("resize should keep the selected match, got %q", m.Body)
	}
}

func TestWrapActivityMessage(t *testing.T) {
	// Python tracebacks separate their lines with a bare \r.
	tb := "[ERROR] RuntimeError: boom\rTraceback (most recent call last):\r  File \"/var/task/app.py\", line 9"
	got := wrapActivityMessage(tb, 80)
	if len(got) != 3 || got[1] != "Traceback (most recent call last):" || got[2] != `  File "/var/task/app.py", line 9` {
		t.Errorf("\\r should start a new line: %q", got)
	}

	// An unbreakable token is hard-broken to the width.
	arn := strings.Repeat("x", 25)
	if got := wrapActivityMessage(arn, 10); len(got) != 3 || got[2] != "xxxxx" {
		t.Errorf("a token wider than the column should hard-break: %q", got)
	}

	// Past the cap the last line says what was left out.
	got = wrapActivityMessage(strings.Repeat("line\n", 20), 60)
	if len(got) != activityWrapMax || got[activityWrapMax-1] != "… +13 more line(s) — y copies the full line" {
		t.Errorf("cap should keep %d lines and report the rest: %q", activityWrapMax, got)
	}
}
