package lambdatui

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("time zone %s unavailable: %v", name, err)
	}
	return loc
}

func TestParseDay(t *testing.T) {
	loc := mustLoc(t, "Australia/Sydney")
	now := time.Date(2026, 9, 25, 10, 30, 0, 0, loc)

	cases := []struct {
		spec, want string
	}{
		{"", "2026-09-25"},
		{"today", "2026-09-25"},
		{" Yesterday ", "2026-09-24"},
		{"2026-01-31", "2026-01-31"},
	}
	for _, c := range cases {
		d, err := ParseDay(c.spec, now, loc)
		if err != nil {
			t.Fatalf("ParseDay(%q): %v", c.spec, err)
		}
		if d.Date != c.want {
			t.Errorf("ParseDay(%q) = %s, want %s", c.spec, d.Date, c.want)
		}
		if d.Start.Hour() != 0 || d.Start.Location() != loc {
			t.Errorf("ParseDay(%q) start = %v, want local midnight", c.spec, d.Start)
		}
	}

	for _, bad := range []string{"2026-13-01", "25/09/2026", "tomorrow"} {
		if _, err := ParseDay(bad, now, loc); err == nil {
			t.Errorf("ParseDay(%q) should fail", bad)
		}
	}
	if _, err := ParseDay("2026-09-26", now, loc); err == nil || !strings.Contains(err.Error(), "future") {
		t.Errorf("a future day must be rejected, got %v", err)
	}

	today, _ := ParseDay("today", now, loc)
	if !today.InProgress(now) {
		t.Error("today should be in progress")
	}
	if y := today.Shift(-1); y.Date != "2026-09-24" || y.InProgress(now) {
		t.Errorf("Shift(-1) = %+v", y)
	}
}

func TestDayHoursAcrossDST(t *testing.T) {
	syd := mustLoc(t, "Australia/Sydney")
	// Sydney springs forward on 2026-10-04: a 23-hour day.
	d, err := ParseDay("2026-10-04", time.Date(2026, 12, 1, 0, 0, 0, 0, syd), syd)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hours() != 23 {
		t.Errorf("DST-start day = %d hours, want 23", d.Hours())
	}
	ny := mustLoc(t, "America/New_York")
	// New York falls back on 2026-11-01: a 25-hour day.
	d, err = ParseDay("2026-11-01", time.Date(2026, 12, 1, 0, 0, 0, 0, ny), ny)
	if err != nil {
		t.Fatal(err)
	}
	if d.Hours() != 25 {
		t.Errorf("DST-end day = %d hours, want 25", d.Hours())
	}
}

func TestParseLambdaLine(t *testing.T) {
	const id = "8f5a3b2c-1d4e-4f6a-9b8c-7d6e5f4a3b2c"
	cases := []struct {
		name, msg          string
		kind               lineKind
		reqID, level, body string
	}{
		{"start", "START RequestId: " + id + " Version: $LATEST", lineStart, id, "", "START RequestId: " + id + " Version: $LATEST"},
		{"end", "END RequestId: " + id, lineEnd, id, "", "END RequestId: " + id},
		{"report", "REPORT RequestId: " + id + "\tDuration: 12.3 ms", lineReport, id, "", "REPORT RequestId: " + id + "\tDuration: 12.3 ms"},
		{"node", "2026-09-24T01:02:03.456Z\t" + id + "\tINFO\tCopied s3://a/k to s3://b/k", lineApp, id, "INFO", "Copied s3://a/k to s3://b/k"},
		{"python logging", "[ERROR]\t2026-09-24T01:02:03.456Z\t" + id + "\tAccessDenied", lineApp, id, "ERROR", "AccessDenied"},
		{"python print", "copied 3 objects", lineApp, "", "", "copied 3 objects"},
		{"plain tabs", "col1\tcol2", lineApp, "", "", "col1\tcol2"},
		{"json app", `{"timestamp":"2026-09-24T01:02:03Z","level":"warn","requestId":"` + id + `","message":"slow copy"}`, lineApp, id, "WARN", "slow copy"},
		{"json object message", `{"level":"INFO","requestId":"` + id + `","message":{"key":"a/b"}}`, lineApp, id, "INFO", `{"key":"a/b"}`},
		{"json platform start", `{"time":"2026-09-24T01:02:03Z","type":"platform.start","record":{"requestId":"` + id + `","version":"$LATEST"}}`, lineStart, id, "", ""},
		{"not json", "{broken", lineApp, "", "", "{broken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseLambdaLine(c.msg)
			if got.kind != c.kind || got.requestID != c.reqID || got.level != c.level {
				t.Errorf("parseLambdaLine = %+v, want kind=%d id=%q level=%q", got, c.kind, c.reqID, c.level)
			}
			if c.body != "" && got.body != c.body {
				t.Errorf("body = %q, want %q", got.body, c.body)
			}
		})
	}
}

func ev(sec int, stream, msg string) LogEvent {
	return LogEvent{Time: time.Unix(1790000000+int64(sec), 0), Stream: stream, Message: msg}
}

func TestLogScanIngest(t *testing.T) {
	const a = "aaaaaaaa-1111-2222-3333-444444444444"
	const b = "bbbbbbbb-1111-2222-3333-444444444444"
	re := regexp.MustCompile(`Copied s3://(?P<src>\S+) to s3://(\S+)`)
	s := NewLogScan(re, "", 0)
	if got := strings.Join(s.GroupNames, ","); got != "src,2" {
		t.Fatalf("group names = %q, want src,2", got)
	}

	more := s.Ingest([]LogEvent{
		ev(1, "s1", "START RequestId: "+a+" Version: $LATEST"),
		ev(2, "s1", "2026-09-24T01:02:03.456Z\t"+a+"\tINFO\tCopied s3://in/x.csv to s3://out/x.csv\n"),
		ev(3, "s2", "START RequestId: "+b+" Version: $LATEST"),
		ev(4, "s2", "Copied s3://in/y.csv to s3://out/y.csv"), // bare print: ID inferred from s2's START
		ev(5, "s3", "Copied s3://in/z.csv to s3://out/z.csv"), // no START seen for s3: no ID
		ev(6, "s1", "END RequestId: "+a),
	})
	if !more {
		t.Fatal("Ingest should ask for more pages")
	}
	if s.Events != 6 || s.Starts != 2 || len(s.Matches) != 3 {
		t.Fatalf("events=%d starts=%d matches=%d", s.Events, s.Starts, len(s.Matches))
	}
	m0 := s.Matches[0]
	if m0.RequestID != a || m0.RequestIDInferred || m0.Level != "INFO" || m0.Groups[0] != "in/x.csv" || m0.Groups[1] != "out/x.csv" {
		t.Errorf("match 0 = %+v", m0)
	}
	if strings.HasSuffix(m0.Message, "\n") {
		t.Error("trailing newline should be trimmed from the message")
	}
	if m1 := s.Matches[1]; m1.RequestID != b || !m1.RequestIDInferred {
		t.Errorf("match 1 should infer the stream's request ID: %+v", m1)
	}
	if m2 := s.Matches[2]; m2.RequestID != "" {
		t.Errorf("match 2 has no START to infer from: %+v", m2)
	}

	if s.Advance(false) || !s.StartsKnown() {
		t.Error("a complete unfiltered scan knows its START count")
	}
	if s.ScanNote() != "" {
		t.Errorf("complete scan note = %q", s.ScanNote())
	}
}

func TestLogScanBounds(t *testing.T) {
	s := NewLogScan(regexp.MustCompile(`hit`), "", 2)
	more := s.Ingest([]LogEvent{ev(1, "s", "hit"), ev(2, "s", "miss"), ev(3, "s", "hit"), ev(4, "s", "hit")})
	if more || !s.Done || len(s.Matches) != 2 {
		t.Fatalf("cap should stop the scan at 2: more=%v done=%v matches=%d", more, s.Done, len(s.Matches))
	}
	if s.StartsKnown() || !strings.Contains(s.ScanNote(), "stopped at 2 matches") {
		t.Errorf("a capped scan must say so: note=%q", s.ScanNote())
	}

	f := NewLogScan(regexp.MustCompile(`x`), `"x"`, 0)
	f.Ingest(nil)
	f.Complete()
	if f.StartsKnown() {
		t.Error("a server-side filter hides START lines, so the count is unknown")
	}
	if !strings.Contains(f.ScanSummary(), "not counted") {
		t.Errorf("summary = %q", f.ScanSummary())
	}

	e := NewLogScan(regexp.MustCompile(`x`), "", 0)
	e.Ingest([]LogEvent{ev(1, "s", "x")})
	e.Fail(errors.New("AccessDenied"))
	if len(e.Matches) != 1 || !strings.Contains(e.ScanNote(), "AccessDenied") {
		t.Errorf("a failed scan keeps its partial matches and reports the error: %+v", e)
	}
}

func TestLogScanPageBudget(t *testing.T) {
	s := NewLogScan(regexp.MustCompile(`x`), "", 0)
	for i := 0; i < maxScanPages; i++ {
		s.Ingest([]LogEvent{ev(i, "s", "y")})
	}
	if s.Advance(false) || s.StopReason != "" || !s.StartsKnown() {
		t.Errorf("a window that ends on the last budgeted page is complete, not cut short: %q", s.StopReason)
	}
	s = NewLogScan(regexp.MustCompile(`x`), "", 0)
	for i := 0; i < maxScanPages; i++ {
		s.Ingest(nil)
	}
	if s.Advance(true) || !strings.Contains(s.ScanNote(), "log pages") {
		t.Errorf("more pages past the budget must stop the scan and say so: %q", s.ScanNote())
	}
}

func TestBuildInvocationStats(t *testing.T) {
	loc := time.UTC
	d, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, loc), loc)
	h := func(n int) time.Time { return d.Start.Add(time.Duration(n) * time.Hour) }
	inv := metricSeries{Timestamps: []time.Time{h(0), h(14), h(23)}, Values: []float64{5, 300, 2}}
	errs := metricSeries{Timestamps: []time.Time{h(14)}, Values: []float64{3}}
	st := buildInvocationStats(d, inv, errs, metricSeries{})

	if len(st.Hours) != 24 || !st.HasData || st.Realigned {
		t.Fatalf("stats = %+v", st)
	}
	if st.Invocations != 307 || st.Errors != 3 || st.Throttles != 0 {
		t.Errorf("totals = %v/%v/%v", st.Invocations, st.Errors, st.Throttles)
	}
	if !math.IsNaN(st.Hours[1].Invocations) {
		t.Error("an hour with no datapoint must stay NaN (no data), not 0")
	}
	if p, ok := st.Peak(); !ok || p.Invocations != 300 || p.Start.Hour() != 14 {
		t.Errorf("peak = %+v", p)
	}

	empty := buildInvocationStats(d, metricSeries{}, metricSeries{}, metricSeries{})
	if empty.HasData {
		t.Error("no datapoints must not read as data")
	}
	if !strings.Contains(empty.InvocationSummary(), "no Invocations datapoints") {
		t.Errorf("summary = %q", empty.InvocationSummary())
	}
}

func TestBucketHoursRealigned(t *testing.T) {
	ist := mustLoc(t, "Asia/Kolkata") // UTC+05:30
	d, _ := ParseDay("2026-01-10", time.Date(2026, 9, 30, 0, 0, 0, 0, ist), ist)
	// Old data comes back on UTC hours: 18:00Z is 23:30 local the day before.
	ts := []time.Time{time.Date(2026, 1, 9, 18, 0, 0, 0, time.UTC), time.Date(2026, 1, 10, 6, 0, 0, 0, time.UTC)}
	vals, realigned := bucketHours(d, metricSeries{Timestamps: ts, Values: []float64{4, 6}})
	if !realigned {
		t.Error("off-boundary datapoints must flag the buckets as realigned")
	}
	if vals[0] != 4 || vals[11] != 6 {
		t.Errorf("buckets = %v", vals)
	}
}

// --- client stubs ---------------------------------------------------------

type stubMetrics struct {
	in  *cloudwatch.GetMetricDataInput
	out *cloudwatch.GetMetricDataOutput
	err error
}

func (s *stubMetrics) GetMetricData(_ context.Context, in *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	s.in = in
	return s.out, s.err
}

type stubLogs struct {
	pages []*cloudwatchlogs.FilterLogEventsOutput
	calls []*cloudwatchlogs.FilterLogEventsInput
	err   error
}

func (s *stubLogs) FilterLogEvents(_ context.Context, in *cloudwatchlogs.FilterLogEventsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	cp := *in
	s.calls = append(s.calls, &cp)
	if s.err != nil {
		return nil, s.err
	}
	p := s.pages[0]
	s.pages = s.pages[1:]
	return p, nil
}

func logEvt(ms int64, stream, msg string) cwltypes.FilteredLogEvent {
	return cwltypes.FilteredLogEvent{Timestamp: aws.Int64(ms), LogStreamName: aws.String(stream), Message: aws.String(msg)}
}

func TestClientInvocationStats(t *testing.T) {
	d, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	m := &stubMetrics{out: &cloudwatch.GetMetricDataOutput{MetricDataResults: []cwtypes.MetricDataResult{
		{Id: aws.String("inv"), Timestamps: []time.Time{d.Start.Add(2 * time.Hour)}, Values: []float64{42}, StatusCode: cwtypes.StatusCodeComplete},
		{Id: aws.String("err"), StatusCode: cwtypes.StatusCodeComplete},
	}}}
	c := &Client{metrics: map[string]metricsAPI{"us-east-1": m}}
	st, err := c.InvocationStats(context.Background(), "us-east-1", "copy", d)
	if err != nil {
		t.Fatal(err)
	}
	if st.Invocations != 42 || st.Hours[2].Invocations != 42 {
		t.Errorf("stats = %+v", st)
	}
	if len(m.in.MetricDataQueries) != 3 || aws.ToInt32(m.in.MetricDataQueries[0].MetricStat.Period) != 3600 {
		t.Errorf("want one batched call with 3 hourly queries, got %+v", m.in.MetricDataQueries)
	}
	if !aws.ToTime(m.in.StartTime).Equal(d.Start) || !aws.ToTime(m.in.EndTime).Equal(d.End) {
		t.Error("the metric window must be the day")
	}

	if _, err := (&Client{}).InvocationStats(context.Background(), "eu-west-1", "copy", d); err == nil {
		t.Error("a region without a client must error, not panic")
	}
}

func TestClientScanLogs(t *testing.T) {
	d, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	base := d.Start.UnixMilli()
	logs := &stubLogs{pages: []*cloudwatchlogs.FilterLogEventsOutput{
		{Events: []cwltypes.FilteredLogEvent{logEvt(base+2000, "s", "Copied b"), logEvt(base+1000, "s", "Copied a")}, NextToken: aws.String("t1")},
		{Events: []cwltypes.FilteredLogEvent{{Message: nil}, logEvt(base+3000, "s", "other")}},
	}}
	c := &Client{logs: map[string]logsAPI{"us-east-1": logs}}
	q := ActivityQuery{Region: "us-east-1", Function: "copy", LogGroup: "/aws/lambda/copy", Day: d, Pattern: regexp.MustCompile(`Copied (\w)`)}
	var progress int
	s := c.ScanLogs(context.Background(), q, func(*LogScan) { progress++ })

	if progress != 2 || len(logs.calls) != 2 {
		t.Fatalf("pages: progress=%d calls=%d", progress, len(logs.calls))
	}
	if aws.ToString(logs.calls[1].NextToken) != "t1" {
		t.Error("the second page must carry the first page's token")
	}
	if aws.ToInt64(logs.calls[0].EndTime) != d.End.UnixMilli()-1 {
		t.Error("EndTime is inclusive, so it must stop 1ms before the next midnight")
	}
	if !s.Done || s.Err != nil || s.Events != 4 || len(s.Matches) != 2 {
		t.Fatalf("scan = %+v", s)
	}
	if s.Matches[0].Groups[0] != "a" {
		t.Errorf("matches must be time-ordered: %+v", s.Matches)
	}

	missing := &stubLogs{err: &smithy.GenericAPIError{Code: "ResourceNotFoundException", Message: "nope"}}
	c.logs["us-east-1"] = missing
	s = c.ScanLogs(context.Background(), q, nil)
	if s.Err == nil || !strings.Contains(s.Err.Error(), "does not exist") {
		t.Errorf("a missing log group should explain itself, got %v", s.Err)
	}
}

// --- rendering ------------------------------------------------------------

func sampleReport(t *testing.T) ActivityReport {
	t.Helper()
	d, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	s := NewLogScan(regexp.MustCompile(`key=(\S+)`), "", 0)
	s.Ingest([]LogEvent{
		{Time: d.Start.Add(90 * time.Minute), Stream: "s", Message: "2026-09-24T01:30:00.000Z\taaaaaaaa-1111-2222-3333-444444444444\tINFO\tkey==cmd|calc"},
	})
	s.Complete()
	return ActivityReport{
		Query: ActivityQuery{Region: "us-east-1", Function: "copy", LogGroup: "/aws/lambda/copy", Day: d, Pattern: s.Pattern},
		Stats: buildInvocationStats(d, metricSeries{Timestamps: []time.Time{d.Start.Add(time.Hour)}, Values: []float64{1234}}, metricSeries{}, metricSeries{}),
		Scan:  s,
		Now:   time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
	}
}

func TestRenderActivityTable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderActivity(&buf, sampleReport(t), "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"1,234 invocations", "01:00  1,234", "00:00  -", "TIME", "$1", "01:30:00.000", "=cmd|calc", "0 START lines", "no datapoint"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderActivityJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderActivity(&buf, sampleReport(t), "json", false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["invocations"].(float64) != 1234 || got["hasMetricData"] != true {
		t.Errorf("json = %v", got)
	}
	hourly := got["hourly"].([]any)
	if hourly[0].(map[string]any)["invocations"] != nil {
		t.Error("an hour with no datapoint must be null in JSON, not 0")
	}
	scan := got["logScan"].(map[string]any)
	m := scan["matches"].([]any)[0].(map[string]any)
	if m["groups"].(map[string]any)["1"] != "=cmd|calc" || m["requestId"] != "aaaaaaaa-1111-2222-3333-444444444444" {
		t.Errorf("match = %v", m)
	}
}

func TestRenderActivityCSVNeutralisesFormulas(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderActivity(&buf, sampleReport(t), "csv", false); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rows[0], ",") != "Time,RequestId,Level,Group_1,Matched,Message,LogStream" {
		t.Errorf("header = %v", rows[0])
	}
	if rows[1][3] != "'=cmd|calc" {
		t.Errorf("a log value starting with = must be neutralised, got %q", rows[1][3])
	}

	// Without a regex, csv falls back to the hourly counts.
	r := sampleReport(t)
	r.Scan = nil
	buf.Reset()
	if err := RenderActivity(&buf, r, "csv", true); err != nil {
		t.Fatal(err)
	}
	rows, _ = csv.NewReader(&buf).ReadAll()
	if len(rows) != 24 || rows[0][1] != "" || rows[1][1] != "1234" {
		t.Errorf("hourly csv = %v", rows[:2])
	}
}

func TestFormatCount(t *testing.T) {
	for in, want := range map[float64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1200: "-1,200"} {
		if got := formatCount(in); got != want {
			t.Errorf("formatCount(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDayProgress(t *testing.T) {
	d, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	s := NewLogScan(regexp.MustCompile(`x`), "", 0)
	after := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	if s.DayProgress(d, after) != 0 {
		t.Error("nothing read yet should be 0")
	}
	s.Ingest([]LogEvent{{Time: d.Start.Add(6 * time.Hour)}, {Time: d.Start.Add(3 * time.Hour)}})
	if got := s.DayProgress(d, after); got != 0.25 {
		t.Errorf("read through 06:00 of a finished day = %v, want 0.25", got)
	}
	// Today, at noon: 06:00 is half of the day so far.
	if got := s.DayProgress(d, d.Start.Add(12*time.Hour)); got != 0.5 {
		t.Errorf("read through 06:00 at noon = %v, want 0.5", got)
	}
}
