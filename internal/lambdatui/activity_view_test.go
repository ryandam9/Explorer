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
	case activityPageMsg, activityMetricsMsg:
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
	mm.act.inputs[actFieldPattern].SetValue("")
	mm.act.inputs[actFieldFilter].SetValue(`"Copied"`)
	mm.Update(key("enter"))
	if !mm.act.formActive || mm.act.formErr == "" {
		t.Error("a server filter without a regex has nothing to show and must be rejected")
	}

	// No regex: metrics only, no log reads.
	mm.act.inputs[actFieldFilter].SetValue("")
	mm.Update(key("enter"))
	if !mm.act.active || mm.act.scan != nil || mm.act.scanning {
		t.Errorf("an empty regex runs metrics only: active=%v scan=%v", mm.act.active, mm.act.scan)
	}
	if !strings.Contains(mm.View(), "No regex given") {
		t.Error("the report should say how to add a regex")
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
