package lambdatui

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Activity answers "what did this function do on this day?" (the `a` key and
// `lambda activity`): how many times it ran — from the AWS/Lambda Invocations
// metric, bucketed per hour — and which of the day's log events match a regex,
// with the request ID, level and capture groups pulled out into columns.
//
// Everything in this file is pure (no AWS calls) so it is table-tested with
// fixture log lines; activity_client.go does the fetching.

// Day is one calendar day in a time zone, as the half-open window
// [Start, End). End is the next local midnight, so a DST-transition day is 23
// or 25 hours long rather than a fixed 24.
type Day struct {
	Date  string // YYYY-MM-DD
	Start time.Time
	End   time.Time
}

// Hours is the number of hour buckets in the day (23, 24 or 25).
func (d Day) Hours() int {
	return int(math.Ceil(d.End.Sub(d.Start).Hours()))
}

// InProgress reports whether now falls inside the day, i.e. the counts are
// for a day that hasn't finished yet.
func (d Day) InProgress(now time.Time) bool {
	return !now.Before(d.Start) && now.Before(d.End)
}

// Shift returns the day n days later (earlier for negative n) in the same zone.
func (d Day) Shift(n int) Day {
	start := d.Start.AddDate(0, 0, n)
	return dayAt(start)
}

// Label renders the day for headers: "Thu 2026-09-24 (AEST, UTC+10:00)".
func (d Day) Label() string {
	zone, _ := d.Start.Zone()
	return fmt.Sprintf("%s %s (%s, UTC%s)", d.Start.Format("Mon"), d.Date, zone, d.Start.Format("-07:00"))
}

func dayAt(t time.Time) Day {
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return Day{
		Date:  start.Format("2006-01-02"),
		Start: start,
		End:   start.AddDate(0, 0, 1),
	}
}

// ParseDay resolves a day spec — "" or "today", "yesterday", or a
// YYYY-MM-DD date — in loc. A day that starts after now is rejected: it can
// have no invocations yet, and an empty answer would read as "never ran".
func ParseDay(spec string, now time.Time, loc *time.Location) (Day, error) {
	if loc == nil {
		loc = time.Local
	}
	now = now.In(loc)
	var d Day
	switch s := strings.ToLower(strings.TrimSpace(spec)); s {
	case "", "today":
		d = dayAt(now)
	case "yesterday":
		d = dayAt(now).Shift(-1)
	default:
		t, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			return Day{}, fmt.Errorf("invalid date %q: use YYYY-MM-DD, today or yesterday", spec)
		}
		d = dayAt(t)
	}
	if d.Start.After(now) {
		return Day{}, fmt.Errorf("date %s is in the future", d.Date)
	}
	return d, nil
}

// ---------------------------------------------------------------------------
// Invocation metrics
// ---------------------------------------------------------------------------

// HourStat is one hour bucket of the day. A NaN value means CloudWatch
// returned no datapoint for that hour — Lambda publishes nothing for an hour
// with no invocations, so it renders as "-" rather than as a measured 0.
type HourStat struct {
	Start       time.Time
	Invocations float64
	Errors      float64
	Throttles   float64
}

// InvocationStats is the day's AWS/Lambda metrics (Sum per hour).
type InvocationStats struct {
	Hours       []HourStat
	Invocations float64 // totals over the hours that had a datapoint
	Errors      float64
	Throttles   float64
	// HasData is false when not a single Invocations datapoint came back: the
	// function was not invoked that day — or the date is past CloudWatch's
	// 455-day retention for hourly data, which the API can't tell apart.
	HasData bool
	// Realigned is set when CloudWatch returned datapoints that don't fall on
	// the day's local hour boundaries: for dates older than 63 days it keeps
	// only hourly data aligned to UTC hours, so in a half-hour-offset zone
	// (e.g. UTC+05:30) each edge of the day is off by that offset.
	Realigned bool
}

// metricSeries is one metric's raw GetMetricData result.
type metricSeries struct {
	Timestamps []time.Time
	Values     []float64
}

// bucketHours places each datapoint in its hour of the day. Buckets with no
// datapoint stay NaN. A datapoint that isn't on a local hour boundary sets
// realigned and goes to the bucket containing it (clamped into the day).
func bucketHours(day Day, s metricSeries) (vals []float64, realigned bool) {
	n := day.Hours()
	vals = make([]float64, n)
	for i := range vals {
		vals[i] = math.NaN()
	}
	for i, ts := range s.Timestamps {
		if i >= len(s.Values) {
			break
		}
		off := ts.Sub(day.Start)
		if off%time.Hour != 0 {
			realigned = true
		}
		idx := int(math.Floor(off.Hours()))
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		if math.IsNaN(vals[idx]) {
			vals[idx] = 0
		}
		vals[idx] += s.Values[i]
	}
	return vals, realigned
}

// buildInvocationStats assembles the per-hour table and totals from the three
// metric series.
func buildInvocationStats(day Day, inv, errs, thr metricSeries) InvocationStats {
	iv, r1 := bucketHours(day, inv)
	ev, r2 := bucketHours(day, errs)
	tv, r3 := bucketHours(day, thr)
	st := InvocationStats{Realigned: r1 || r2 || r3}
	for i := range iv {
		st.Hours = append(st.Hours, HourStat{
			Start:       day.Start.Add(time.Duration(i) * time.Hour),
			Invocations: iv[i],
			Errors:      ev[i],
			Throttles:   tv[i],
		})
		if !math.IsNaN(iv[i]) {
			st.HasData = true
			st.Invocations += iv[i]
		}
		if !math.IsNaN(ev[i]) {
			st.Errors += ev[i]
		}
		if !math.IsNaN(tv[i]) {
			st.Throttles += tv[i]
		}
	}
	return st
}

// Peak returns the busiest hour (ok=false when there is no data).
func (s InvocationStats) Peak() (HourStat, bool) {
	var best HourStat
	found := false
	for _, h := range s.Hours {
		if math.IsNaN(h.Invocations) {
			continue
		}
		if !found || h.Invocations > best.Invocations {
			best, found = h, true
		}
	}
	return best, found
}

// HourlyInvocations returns the per-hour invocation series (NaN = no data) for
// the sparkline.
func (s InvocationStats) HourlyInvocations() []float64 {
	out := make([]float64, len(s.Hours))
	for i, h := range s.Hours {
		out[i] = h.Invocations
	}
	return out
}

// ---------------------------------------------------------------------------
// Log scan
// ---------------------------------------------------------------------------

// LogEvent is one CloudWatch Logs event, decoupled from the SDK type.
type LogEvent struct {
	Time    time.Time
	Stream  string
	Message string
}

// Match is one log event the regex matched.
type Match struct {
	Time      time.Time
	Stream    string
	RequestID string
	// RequestIDInferred is set when the line itself carried no request ID and
	// it was taken from the stream's most recent START line (an execution
	// environment runs one invocation at a time, so its lines between two
	// STARTs belong to the first one).
	RequestIDInferred bool
	Level             string
	Body              string   // the message with the runtime's timestamp/ID/level prefix removed
	Message           string   // the full raw message
	Matched           string   // the text the regex matched
	Groups            []string // capture groups, parallel to LogScan.GroupNames
}

// Scan limits. A day of a busy function can hold millions of lines, so the
// scan is bounded and says so when a bound ends it early.
const (
	// DefaultMaxMatches caps the matches kept (and ends the scan when reached).
	DefaultMaxMatches = 1000
	// maxScanPages bounds the FilterLogEvents pages read (each is up to 10,000
	// events / 1 MB).
	maxScanPages = 1000
)

// LogScan accumulates a day's log events page by page: it counts events and
// START lines and keeps the events the regex matches. It is fed by both the
// TUI's pull loop and the CLI's loop, so neither holds the whole day in
// memory.
type LogScan struct {
	Pattern    *regexp.Regexp
	GroupNames []string // column label per capture group ("1", "2" or the group's name)
	Filter     string   // server-side CloudWatch filter pattern ("" = none)
	MaxMatches int

	Events  int // events read
	Starts  int // START lines seen (one per invocation that logged)
	Pages   int
	Matches []Match

	Done       bool   // the scan finished (window exhausted or a bound reached)
	StopReason string // why it ended early ("" when the whole day was read)
	Err        error

	streamReq map[string]string // stream → request ID of its latest START
}

// NewLogScan prepares a scan. maxMatches <= 0 uses DefaultMaxMatches.
func NewLogScan(pattern *regexp.Regexp, filter string, maxMatches int) *LogScan {
	if maxMatches <= 0 {
		maxMatches = DefaultMaxMatches
	}
	return &LogScan{
		Pattern:    pattern,
		GroupNames: groupNames(pattern),
		Filter:     filter,
		MaxMatches: maxMatches,
		streamReq:  map[string]string{},
	}
}

func groupNames(re *regexp.Regexp) []string {
	if re == nil {
		return nil
	}
	names := re.SubexpNames()
	out := make([]string, 0, len(names))
	for i := 1; i < len(names); i++ {
		if names[i] != "" {
			out = append(out, names[i])
		} else {
			out = append(out, fmt.Sprintf("%d", i))
		}
	}
	return out
}

// Ingest folds one page of events into the scan. It reports false once the
// match cap is reached (the caller should stop paging); otherwise the caller
// asks Advance whether to fetch the next page.
func (s *LogScan) Ingest(events []LogEvent) bool {
	s.Pages++
	for _, ev := range events {
		s.Events++
		msg := strings.TrimRight(ev.Message, "\r\n")
		line := parseLambdaLine(msg)
		if line.kind == lineStart {
			s.Starts++
			if line.requestID != "" {
				s.streamReq[ev.Stream] = line.requestID
			}
		}
		if s.Pattern == nil {
			continue
		}
		loc := s.Pattern.FindStringSubmatchIndex(msg)
		if loc == nil {
			continue
		}
		m := Match{
			Time:      ev.Time,
			Stream:    ev.Stream,
			RequestID: line.requestID,
			Level:     line.level,
			Body:      line.body,
			Message:   msg,
			Matched:   msg[loc[0]:loc[1]],
		}
		if m.RequestID == "" {
			if id, ok := s.streamReq[ev.Stream]; ok {
				m.RequestID, m.RequestIDInferred = id, true
			}
		}
		for g := 1; 2*g+1 < len(loc); g++ {
			var v string
			if loc[2*g] >= 0 {
				v = msg[loc[2*g]:loc[2*g+1]]
			}
			m.Groups = append(m.Groups, v)
		}
		s.Matches = append(s.Matches, m)
		if len(s.Matches) >= s.MaxMatches {
			s.finish(fmt.Sprintf("stopped at %d matches — more may exist later in the day", s.MaxMatches))
			return false
		}
	}
	return true
}

// Advance decides whether to read another page after one was ingested:
// hasNext=false means the day's window is exhausted (the scan is complete);
// otherwise it continues unless the page budget is spent. The budget is
// checked here, not in Ingest, so a day that ends exactly on the last
// budgeted page reads as complete rather than cut short.
func (s *LogScan) Advance(hasNext bool) bool {
	if !hasNext {
		s.Complete()
		return false
	}
	if s.Pages >= maxScanPages {
		s.finish(fmt.Sprintf("stopped after %d log pages (%d events) — narrow the scan with a server-side filter", s.Pages, s.Events))
		return false
	}
	return true
}

func (s *LogScan) finish(reason string) {
	s.Done = true
	s.StopReason = reason
}

// Complete marks the day's window as fully read.
func (s *LogScan) Complete() { s.Done = true }

// Fail records a scan error; what was read so far is kept as a partial result.
func (s *LogScan) Fail(err error) {
	s.Done = true
	s.Err = err
}

// StartsKnown reports whether Starts is the day's full START-line count: the
// whole window was read without a server-side filter (which would hide START
// lines) and without error.
func (s *LogScan) StartsKnown() bool {
	return s.Done && s.StopReason == "" && s.Err == nil && s.Filter == ""
}

// SortMatches orders matches by time (FilterLogEvents interleaves streams, so
// this is a guard, not a reorder in the common case).
func (s *LogScan) SortMatches() {
	sort.SliceStable(s.Matches, func(i, j int) bool { return s.Matches[i].Time.Before(s.Matches[j].Time) })
}

// ---------------------------------------------------------------------------
// Lambda log-line parsing
// ---------------------------------------------------------------------------

type lineKind int

const (
	lineApp lineKind = iota
	lineStart
	lineEnd
	lineReport
)

type lambdaLine struct {
	kind      lineKind
	requestID string
	level     string
	body      string
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var logLevels = map[string]bool{
	"TRACE": true, "DEBUG": true, "INFO": true, "WARN": true, "WARNING": true,
	"ERROR": true, "FATAL": true, "CRITICAL": true,
}

// parseLambdaLine pulls the request ID, level and body out of a Lambda log
// line. It understands the platform lines (START/END/REPORT RequestId: …), the
// managed runtimes' tab-separated text format — Node.js
// "<ts>\t<requestId>\t<LEVEL>\t<msg>" and Python's
// "[LEVEL]\t<ts>\t<requestId>\t<msg>" — and the JSON log format
// ({"level","requestId","message"} and {"type":"platform.start","record":…}).
// Anything else (a bare print()) is returned as-is with no ID.
func parseLambdaLine(msg string) lambdaLine {
	for _, p := range []struct {
		prefix string
		kind   lineKind
	}{{"START RequestId: ", lineStart}, {"END RequestId: ", lineEnd}, {"REPORT RequestId: ", lineReport}} {
		if strings.HasPrefix(msg, p.prefix) {
			rest := msg[len(p.prefix):]
			id := rest
			if i := strings.IndexAny(rest, " \t"); i >= 0 {
				id = rest[:i]
			}
			return lambdaLine{kind: p.kind, requestID: id, body: msg}
		}
	}
	if strings.HasPrefix(strings.TrimSpace(msg), "{") {
		if l, ok := parseJSONLine(msg); ok {
			return l
		}
	}
	fields := strings.Split(msg, "\t")
	if len(fields) < 2 {
		return lambdaLine{body: msg}
	}
	var l lambdaLine
	consumed := 0
	// The prefix is at most three fields (timestamp, request ID, level) in
	// either order; stop at the first field that is none of them.
	for consumed < len(fields)-1 && consumed < 3 {
		f := strings.TrimSpace(fields[consumed])
		switch {
		case l.requestID == "" && uuidRe.MatchString(f):
			l.requestID = f
		case l.level == "" && logLevels[strings.ToUpper(strings.Trim(f, "[]"))]:
			l.level = strings.ToUpper(strings.Trim(f, "[]"))
		case looksLikeTimestamp(f):
		default:
			l.body = strings.Join(fields[consumed:], "\t")
			return l
		}
		consumed++
	}
	if l.requestID == "" && l.level == "" {
		return lambdaLine{body: msg}
	}
	l.body = strings.Join(fields[consumed:], "\t")
	return l
}

func looksLikeTimestamp(s string) bool {
	if len(s) < 19 {
		return false
	}
	_, err := time.Parse("2006-01-02T15:04:05", s[:19])
	return err == nil
}

func parseJSONLine(msg string) (lambdaLine, bool) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(msg), &obj); err != nil {
		return lambdaLine{}, false
	}
	str := func(m map[string]any, k string) string {
		if v, ok := m[k].(string); ok {
			return v
		}
		return ""
	}
	if typ := str(obj, "type"); strings.HasPrefix(typ, "platform.") {
		l := lambdaLine{body: msg}
		if rec, ok := obj["record"].(map[string]any); ok {
			l.requestID = str(rec, "requestId")
		}
		switch typ {
		case "platform.start":
			l.kind = lineStart
		case "platform.runtimeDone":
			l.kind = lineEnd
		case "platform.report":
			l.kind = lineReport
		}
		return l, true
	}
	l := lambdaLine{
		requestID: firstNonEmpty(str(obj, "requestId"), str(obj, "AWSRequestId"), str(obj, "aws_request_id")),
		level:     strings.ToUpper(firstNonEmpty(str(obj, "level"), str(obj, "levelname"))),
		body:      msg,
	}
	switch v := obj["message"].(type) {
	case string:
		l.body = v
	case nil:
	default:
		if b, err := json.Marshal(v); err == nil {
			l.body = string(b)
		}
	}
	return l, true
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
