package lambdatui

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Invocations: which request IDs failed, what a REPORT line says about a run,
// and which of a stream's lines make up one invocation. All pure, so the
// drill-down (Enter on a match), the failed-invocation jump (E) and the
// performance summary are fixture-tested.

// Failure reasons, most severe last: an invocation keeps the worst one seen.
const (
	failErrorLogged = "logged an error"
	failRuntimeExit = "runtime exited"
	failTimeout     = "timed out"
)

var failureRank = map[string]int{failErrorLogged: 1, failRuntimeExit: 2, failTimeout: 3}

// failureOf reports why a log line shows its invocation failed, or "". Only
// unambiguous signals count — the platform's REPORT status, "Task timed out",
// a runtime exit, and a line logged at ERROR/FATAL/CRITICAL — so a word like
// "error" inside an INFO message never marks a run as failed.
func failureOf(l lambdaLine) string {
	if l.kind == lineReport || l.kind == lineEnd {
		switch reportStatus(l.body) {
		case "timeout":
			return failTimeout
		case "error", "failure":
			return failRuntimeExit
		}
		return ""
	}
	switch {
	case strings.Contains(l.body, "Task timed out after"):
		return failTimeout
	case strings.Contains(l.body, "Runtime exited"), strings.Contains(l.body, "Runtime.ExitError"):
		return failRuntimeExit
	case l.level == "ERROR" || l.level == "FATAL" || l.level == "CRITICAL":
		return failErrorLogged
	}
	return ""
}

// noteFailure records a failure signal against its invocation, keeping the
// most severe reason.
func (s *LogScan) noteFailure(id string, l lambdaLine) {
	if id == "" {
		return
	}
	reason := failureOf(l)
	if reason == "" {
		return
	}
	if cur, ok := s.failed[id]; !ok || failureRank[reason] > failureRank[cur] {
		s.failed[id] = reason
	}
}

// Failure returns why the invocation failed ("" when no failure was seen in
// the lines read).
func (s *LogScan) Failure(requestID string) string {
	if s == nil || requestID == "" {
		return ""
	}
	return s.failed[requestID]
}

// FailedCount is how many distinct invocations showed a failure signal.
func (s *LogScan) FailedCount() int {
	if s == nil {
		return 0
	}
	return len(s.failed)
}

// ---------------------------------------------------------------------------
// REPORT lines
// ---------------------------------------------------------------------------

// InvocationReport is what the platform's REPORT line says about one run.
// Zero durations with Has* false mean the field was absent, not zero.
type InvocationReport struct {
	RequestID    string
	DurationMs   float64
	BilledMs     float64
	MemorySizeMB float64
	MaxMemoryMB  float64
	InitMs       float64 // only on a cold start
	ColdStart    bool    // an Init Duration was reported
	Status       string  // "success", "timeout", "error"… ("" when not reported)
}

var reportFieldRe = regexp.MustCompile(`(Duration|Billed Duration|Memory Size|Max Memory Used|Init Duration|Status): ([^\t]+?)\s*(?:\t|$)`)

// reportStatus pulls the run's status out of a REPORT/END line ("" when the
// line carries none — older runtimes print no Status field).
func reportStatus(body string) string {
	if r, ok := parseReport(body); ok {
		return r.Status
	}
	return ""
}

// parseReport reads a REPORT line in the text format
// ("REPORT RequestId: … Duration: 12.3 ms Billed Duration: 13 ms …") or the
// JSON log format ({"type":"platform.report","record":{…}}).
func parseReport(body string) (InvocationReport, bool) {
	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, "{") {
		return parseJSONReport(body)
	}
	if !strings.HasPrefix(body, "REPORT RequestId: ") && !strings.HasPrefix(body, "END RequestId: ") {
		return InvocationReport{}, false
	}
	var r InvocationReport
	rest := body[strings.Index(body, ": ")+2:]
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		r.RequestID = rest[:i]
	} else {
		r.RequestID = rest
	}
	num := func(v string) float64 {
		f, _ := strconv.ParseFloat(strings.Fields(v)[0], 64)
		return f
	}
	for _, m := range reportFieldRe.FindAllStringSubmatch(body, -1) {
		switch m[1] {
		case "Duration":
			r.DurationMs = num(m[2])
		case "Billed Duration":
			r.BilledMs = num(m[2])
		case "Memory Size":
			r.MemorySizeMB = num(m[2])
		case "Max Memory Used":
			r.MaxMemoryMB = num(m[2])
		case "Init Duration":
			r.InitMs, r.ColdStart = num(m[2]), true
		case "Status":
			r.Status = strings.ToLower(strings.TrimRight(strings.Fields(m[2])[0], ","))
		}
	}
	return r, true
}

func parseJSONReport(body string) (InvocationReport, bool) {
	var obj struct {
		Type   string `json:"type"`
		Record struct {
			RequestID string `json:"requestId"`
			Status    string `json:"status"`
			Metrics   struct {
				DurationMs       float64  `json:"durationMs"`
				BilledDurationMs float64  `json:"billedDurationMs"`
				MemorySizeMB     float64  `json:"memorySizeMB"`
				MaxMemoryUsedMB  float64  `json:"maxMemoryUsedMB"`
				InitDurationMs   *float64 `json:"initDurationMs"`
			} `json:"metrics"`
		} `json:"record"`
	}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return InvocationReport{}, false
	}
	if obj.Type != "platform.report" && obj.Type != "platform.runtimeDone" {
		return InvocationReport{}, false
	}
	rec := obj.Record
	r := InvocationReport{
		RequestID:    rec.RequestID,
		Status:       strings.ToLower(rec.Status),
		DurationMs:   rec.Metrics.DurationMs,
		BilledMs:     rec.Metrics.BilledDurationMs,
		MemorySizeMB: rec.Metrics.MemorySizeMB,
		MaxMemoryMB:  rec.Metrics.MaxMemoryUsedMB,
	}
	if rec.Metrics.InitDurationMs != nil {
		r.InitMs, r.ColdStart = *rec.Metrics.InitDurationMs, true
	}
	return r, true
}

// ---------------------------------------------------------------------------
// One invocation's lines
// ---------------------------------------------------------------------------

// Invocation is the lines of one run, read back from its log stream.
type Invocation struct {
	RequestID string
	Stream    string
	Lines     []Match // Match reused for its parsed fields (Groups/Matched unused)
	Report    *InvocationReport
	Failure   string // why it failed, from its own lines ("" = no failure seen)
	HasStart  bool   // the START line was inside the window read
	HasEnd    bool   // the REPORT line was inside the window read
}

// extractInvocation picks one invocation's lines out of its stream's events
// (in time order). An execution environment runs one invocation at a time, so
// everything between START <id> and REPORT <id> belongs to it — including
// print() lines with no ID. Output with no ID just before a cold START (the
// init phase: INIT_START, module-level prints) is kept too, since it ran for
// this invocation. Lines tagged with a different request ID are left out.
func extractInvocation(events []LogEvent, requestID string) Invocation {
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	inv := Invocation{RequestID: requestID}
	var pending []Match // ID-less lines since the previous invocation ended
	inside := false
	for _, ev := range events {
		msg := strings.TrimRight(ev.Message, "\r\n")
		l := parseLambdaLine(msg)
		m := Match{Time: ev.Time, Stream: ev.Stream, RequestID: l.requestID, Level: l.level, Body: l.body, Message: msg}
		if inv.Stream == "" {
			inv.Stream = ev.Stream
		}
		switch {
		case l.kind == lineStart && l.requestID == requestID:
			inside, inv.HasStart = true, true
			inv.Lines = append(inv.Lines, pending...)
			pending = nil
			inv.Lines = append(inv.Lines, m)
		case l.kind == lineStart:
			inside = false // another invocation began; ours is over
			pending = nil
		case l.requestID == requestID:
			inv.Lines = append(inv.Lines, m)
			if l.kind == lineReport {
				inv.HasEnd = true
				if r, ok := parseReport(l.body); ok {
					inv.Report = &r
				}
				inside = false
			}
		case l.requestID != "":
			// Another invocation's line (or its END/REPORT): not ours, and it
			// closes the init window for whatever comes next.
			pending = nil
			if l.kind == lineReport || l.kind == lineEnd {
				inside = false
			}
		case inside:
			m.RequestID, m.RequestIDInferred = requestID, true
			inv.Lines = append(inv.Lines, m)
		default:
			pending = append(pending, m)
		}
	}
	for _, m := range inv.Lines {
		if r := failureOf(parseLambdaLine(m.Message)); failureRank[r] > failureRank[inv.Failure] {
			inv.Failure = r
		}
	}
	return inv
}

// invocationWindow is the slice of the stream to read for the invocation a
// line at t belongs to: every line of a run lies within one timeout of any
// other, so [t-timeout, t+timeout] (plus a margin for clock rounding and the
// init phase) holds all of it.
func invocationWindow(t time.Time, timeout time.Duration) (start, end time.Time) {
	if timeout <= 0 {
		timeout = 15 * time.Minute // Lambda's maximum
	}
	margin := 30 * time.Second
	return t.Add(-timeout - margin), t.Add(timeout + margin)
}
