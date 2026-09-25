package lambdatui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

const (
	reqA = "3f772d5c-ddd1-4e9f-96e2-661d2ac65c6c"
	reqB = "28d42b53-533d-4488-8b05-f4f1929e9014"
	reqC = "1816cb63-aed5-441b-8c34-7afa9f57c785"
)

func TestFailureOf(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"[ERROR]\t2026-09-23T21:35:37.940Z\t" + reqA + "\tFailed to copy", failErrorLogged},
		{"[ERROR] RuntimeError: boom\rTraceback (most recent call last):", failErrorLogged},
		{"2026-09-23T21:35:37.940Z " + reqA + " Task timed out after 3.00 seconds", failTimeout},
		{"RequestId: " + reqA + " Error: Runtime exited with error: signal: killed\nRuntime.ExitError", failRuntimeExit},
		{"REPORT RequestId: " + reqA + "\tDuration: 3000.00 ms\tBilled Duration: 3000 ms\tMemory Size: 128 MB\tMax Memory Used: 60 MB\tStatus: timeout", failTimeout},
		{`{"type":"platform.report","record":{"requestId":"` + reqA + `","status":"error","metrics":{"durationMs":5}}}`, failRuntimeExit},
		{`{"level":"ERROR","requestId":"` + reqA + `","message":"boom"}`, failErrorLogged},
		// The word "error" inside an INFO line is not a failure signal.
		{"[INFO]\t2026-09-23T21:35:37.940Z\t" + reqA + "\tretrying after error 503", ""},
		{"REPORT RequestId: " + reqA + "\tDuration: 5.00 ms\tBilled Duration: 6 ms\tMemory Size: 128 MB\tMax Memory Used: 60 MB", ""},
	}
	for _, c := range cases {
		if got := failureOf(parseLambdaLine(c.line)); got != c.want {
			t.Errorf("failureOf(%.60q) = %q, want %q", c.line, got, c.want)
		}
	}
}

func TestScanTracksFailedInvocations(t *testing.T) {
	scan := NewLogScan(regexp.MustCompile(`Copied`), "", 100)
	scan.Ingest([]LogEvent{
		ev(0, "s1", "START RequestId: "+reqA+" Version: $LATEST"),
		ev(1, "s1", "[INFO]\t2026-09-23T21:35:37.940Z\t"+reqA+"\tCopied a.csv"),
		ev(2, "s1", "[ERROR] RuntimeError: boom\rTraceback (most recent call last):"), // no ID: inherits A
		ev(3, "s1", "REPORT RequestId: "+reqA+"\tDuration: 5.00 ms"),
		ev(4, "s2", "START RequestId: "+reqB+" Version: $LATEST"),
		ev(5, "s2", "[INFO]\t2026-09-23T21:35:40.000Z\t"+reqB+"\tCopied b.csv"),
		ev(6, "s2", "2026-09-23T21:35:43.000Z "+reqB+" Task timed out after 3.00 seconds"),
		ev(7, "s3", "START RequestId: "+reqC+" Version: $LATEST"),
		ev(8, "s3", "[INFO]\t2026-09-23T21:35:44.000Z\t"+reqC+"\tCopied c.csv"),
	})
	if scan.FailedCount() != 2 || scan.Failure(reqA) != failErrorLogged || scan.Failure(reqB) != failTimeout || scan.Failure(reqC) != "" {
		t.Errorf("failed = %v", scan.failed)
	}
	if got := parseLambdaLine("[ERROR] RuntimeError: boom").level; got != "ERROR" {
		t.Errorf("a [LEVEL]-prefixed line with no tabs should keep its level, got %q", got)
	}
}

func TestParseReport(t *testing.T) {
	r, ok := parseReport("REPORT RequestId: " + reqA + "\tDuration: 1945.40 ms\tBilled Duration: 2275 ms\tMemory Size: 256 MB\tMax Memory Used: 104 MB\tInit Duration: 329.43 ms\t")
	if !ok || r.RequestID != reqA || r.DurationMs != 1945.40 || r.BilledMs != 2275 || r.MemorySizeMB != 256 ||
		r.MaxMemoryMB != 104 || !r.ColdStart || r.InitMs != 329.43 || r.Status != "" {
		t.Errorf("text REPORT = %+v", r)
	}
	r, ok = parseReport(`{"time":"2026-09-24T00:00:00Z","type":"platform.report","record":{"requestId":"` + reqB +
		`","metrics":{"durationMs":12.5,"billedDurationMs":13,"memorySizeMB":128,"maxMemoryUsedMB":70},"status":"success"}}`)
	if !ok || r.RequestID != reqB || r.DurationMs != 12.5 || r.BilledMs != 13 || r.ColdStart || r.Status != "success" {
		t.Errorf("JSON REPORT = %+v", r)
	}
	if _, ok := parseReport("START RequestId: " + reqA); ok {
		t.Error("a START line is not a report")
	}
}

// One stream, three runs: a cold start (init output, then A), B, then a line
// from C. Only A's lines — init output included — come back for A.
func TestExtractInvocation(t *testing.T) {
	events := []LogEvent{
		ev(0, "s", "INIT_START Runtime Version: python:3.12.v31"),
		ev(0, "s", "loading model at import time"), // module-level print during init
		ev(1, "s", "START RequestId: "+reqA+" Version: $LATEST"),
		ev(2, "s", "[INFO]\t2026-09-24T00:00:02.000Z\t"+reqA+"\tCopied a.csv"),
		ev(3, "s", "bare print inside A"),
		ev(4, "s", "END RequestId: "+reqA),
		ev(4, "s", "REPORT RequestId: "+reqA+"\tDuration: 3000.00 ms\tBilled Duration: 3000 ms\tMemory Size: 128 MB\tMax Memory Used: 60 MB\tInit Duration: 200.00 ms"),
		ev(5, "s", "START RequestId: "+reqB+" Version: $LATEST"),
		ev(6, "s", "bare print inside B"),
		ev(7, "s", "REPORT RequestId: "+reqB+"\tDuration: 5.00 ms"),
	}
	inv := extractInvocation(events, reqA)
	var bodies []string
	for _, l := range inv.Lines {
		bodies = append(bodies, l.Body)
	}
	want := []string{"INIT_START Runtime Version: python:3.12.v31", "loading model at import time",
		"START RequestId: " + reqA + " Version: $LATEST", "Copied a.csv", "bare print inside A", "END RequestId: " + reqA}
	if len(bodies) != len(want)+1 || strings.Join(bodies[:len(want)], "|") != strings.Join(want, "|") ||
		!strings.HasPrefix(bodies[len(want)], "REPORT") {
		t.Fatalf("lines = %q", bodies)
	}
	if !inv.HasStart || !inv.HasEnd || inv.Report == nil || inv.Report.DurationMs != 3000 || !inv.Report.ColdStart {
		t.Errorf("invocation = %+v report %+v", inv, inv.Report)
	}

	// B: a warm start — no init lines, and nothing of A.
	invB := extractInvocation(events, reqB)
	if len(invB.Lines) != 3 || invB.Lines[1].Body != "bare print inside B" || !invB.Lines[1].RequestIDInferred {
		t.Errorf("B lines = %+v", invB.Lines)
	}
}

// Enter on a match reads its invocation back from the stream (one
// FilterLogEvents call scoped to that stream and a ±timeout window) and opens
// on the matched line; E jumps between matches whose invocation failed.
func TestActivityInvocationDrillDown(t *testing.T) {
	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{
			logEvt(1790000000000, "s1", "START RequestId: "+reqA+" Version: $LATEST"),
			logEvt(1790000001000, "s1", "[INFO]\t2026-09-24T00:00:01.000Z\t"+reqA+"\tCopied a.csv"),
			logEvt(1790000002000, "s2", "START RequestId: "+reqB+" Version: $LATEST"),
			logEvt(1790000003000, "s2", "[INFO]\t2026-09-24T00:00:03.000Z\t"+reqB+"\tCopied b.csv"),
			logEvt(1790000004000, "s2", "[ERROR]\t2026-09-24T00:00:04.000Z\t"+reqB+"\tupload failed"),
			logEvt(1790000005000, "s3", "START RequestId: "+reqC+" Version: $LATEST"),
			logEvt(1790000006000, "s3", "[INFO]\t2026-09-24T00:00:06.000Z\t"+reqC+"\tCopied c.csv"),
		}},
	}}
	mm := newActivityTestModel(logs)
	mm.inv.Functions[0].TimeoutSec = 60
	mm.rebuild()
	mm.Update(key("a"))
	mm.act.inputs[actFieldPattern].SetValue(`Copied`)
	_, cmd := mm.Update(key("enter"))
	runCmd(mm, cmd)
	if mm.act.scan.FailedCount() != 1 || len(mm.act.scan.Matches) != 3 {
		t.Fatalf("scan: %d failed, %d matches", mm.act.scan.FailedCount(), len(mm.act.scan.Matches))
	}
	if cell := mm.act.tbl.Rows()[1][1]; !strings.HasPrefix(cell, "✗ ") {
		t.Errorf("the failed invocation's row should be marked: %q", cell)
	}

	// E: from the first match to B's (the failed one), then wrapping back to it.
	mm.Update(key("E"))
	if m, _ := mm.selectedMatch(); m.RequestID != reqB {
		t.Fatalf("E should land on the failed invocation's match, got %s", m.RequestID)
	}
	mm.Update(key("E"))
	if m, _ := mm.selectedMatch(); m.RequestID != reqB {
		t.Errorf("E with one failed match should wrap back to it, got %s", m.RequestID)
	}

	// Enter: read B's stream back.
	logs.pages = append(logs.pages, &cloudwatchlogs.FilterLogEventsOutput{Events: []cwltypes.FilteredLogEvent{
		logEvt(1790000002000, "s2", "START RequestId: "+reqB+" Version: $LATEST"),
		logEvt(1790000003000, "s2", "[INFO]\t2026-09-24T00:00:03.000Z\t"+reqB+"\tCopied b.csv"),
		logEvt(1790000003500, "s2", "retrying upload"),
		logEvt(1790000004000, "s2", "[ERROR]\t2026-09-24T00:00:04.000Z\t"+reqB+"\tupload failed"),
		logEvt(1790000004100, "s2", "REPORT RequestId: "+reqB+"\tDuration: 2100.00 ms\tBilled Duration: 2100 ms\tMemory Size: 128 MB\tMax Memory Used: 64 MB"),
	}})
	_, cmd = mm.Update(key("enter"))
	if !mm.act.inv.active || !mm.act.inv.loading {
		t.Fatal("Enter should open the invocation view")
	}
	runCmd(mm, cmd)
	call := logs.calls[len(logs.calls)-1]
	if len(call.LogStreamNames) != 1 || call.LogStreamNames[0] != "s2" ||
		aws.ToInt64(call.EndTime)-aws.ToInt64(call.StartTime) != (2*60+60)*1000 {
		t.Errorf("the read should be scoped to s2 and ±(timeout+30s): %+v", call)
	}
	v := mm.act.inv
	if v.loading || v.err != nil || len(v.inv.Lines) != 5 || v.inv.Failure != failErrorLogged || v.inv.Report == nil {
		t.Fatalf("invocation = %+v (err %v)", v.inv, v.err)
	}
	if i := v.tbl.CursorGroup(); v.inv.Lines[i].Body != "Copied b.csv" {
		t.Errorf("the view should open on the matched line, got %q", v.inv.Lines[i].Body)
	}
	view := mm.View().Content
	for _, want := range []string{"Invocation " + reqB, "retrying upload", "✗ this invocation logged an error", "duration 2.10 s", "of 60s timeout"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// Esc returns to the matches with the cursor where it was.
	mm.Update(key("esc"))
	if mm.act.inv.active || !mm.act.active {
		t.Error("Esc should close only the invocation view")
	}
	if m, _ := mm.selectedMatch(); m.RequestID != reqB {
		t.Error("the report's selection should be kept")
	}
}
