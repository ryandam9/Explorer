package lambdatui

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func report(id string, durMs, billedMs, sizeMB, usedMB float64, initMs float64) string {
	s := "REPORT RequestId: " + id + "\tDuration: " + ftoa(durMs) + " ms\tBilled Duration: " + ftoa(billedMs) +
		" ms\tMemory Size: " + ftoa(sizeMB) + " MB\tMax Memory Used: " + ftoa(usedMB) + " MB\t"
	if initMs > 0 {
		s += "Init Duration: " + ftoa(initMs) + " ms\t"
	}
	return s
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func TestPerfStatsFromReports(t *testing.T) {
	scan := NewLogScan(matchAll, "", 1000)
	var evs []LogEvent
	// Ten runs of 100..1000 ms at 256 MB, billed to the next ms; runs 1 and 6
	// are cold starts.
	for i := 1; i <= 10; i++ {
		init := 0.0
		if i == 1 || i == 6 {
			init = 300 + float64(i)
		}
		evs = append(evs, ev(i, "s", report("id-"+ftoa(float64(i)), float64(i*100), float64(i*100), 256, float64(90+i), init)))
	}
	scan.Ingest(evs)
	scan.Advance(false)
	p := &scan.Perf
	if p.Count != 10 || p.DurationP(50) != 500 || p.DurationP(95) != 1000 || p.MaxDurationMs() != 1000 || p.MaxMemoryMB() != 100 {
		t.Errorf("stats: n=%d p50=%v p95=%v max=%v mem=%v", p.Count, p.DurationP(50), p.DurationP(95), p.MaxDurationMs(), p.MaxMemoryMB())
	}
	if p.ColdStarts != 2 || p.InitAvgMs() != 303.5 || p.InitMaxMs != 306 {
		t.Errorf("cold starts: %d avg %v max %v", p.ColdStarts, p.InitAvgMs(), p.InitMaxMs)
	}
	// 5,500 ms billed at 0.25 GB = 1.375 GB-s.
	if math.Abs(p.GBSeconds-1.375) > 1e-9 {
		t.Errorf("GB-s = %v, want 1.375", p.GBSeconds)
	}
	wantX86 := 1.375*priceGBSecondX86 + 10*pricePerRequest
	if math.Abs(p.EstimatedCost("x86_64")-wantX86) > 1e-12 || p.EstimatedCost("arm64") >= p.EstimatedCost("x86_64") {
		t.Errorf("cost x86 %v (want %v), arm64 %v", p.EstimatedCost("x86_64"), wantX86, p.EstimatedCost("arm64"))
	}

	lines := perfLines(scan, 2, "x86_64")
	if len(lines) != 2 || !strings.Contains(lines[0], "p50 500 ms · p95 1.00 s · max 1.00 s — 50% of the 2s timeout") ||
		!strings.Contains(lines[1], "max 100 of 256 MB (39%)") || !strings.Contains(lines[1], "2 cold start(s), init avg 304 ms") {
		t.Errorf("perf lines = %q", lines)
	}
}

// A server filter hides REPORT lines: the figures are "not measured", and the
// JSON carries nulls rather than zeros.
func TestPerfNotMeasuredUnderFilter(t *testing.T) {
	scan := NewLogScan(matchAll, `"Copied"`, 1000)
	scan.Ingest([]LogEvent{ev(1, "s", "[INFO]\t2026-09-24T00:00:01.000Z\t"+reqA+"\tCopied a.csv")})
	scan.Advance(false)
	if scan.PerfKnown() {
		t.Fatal("with a filter and no REPORT line, performance is not measured")
	}
	if l := perfLines(scan, 60, "x86_64"); len(l) != 1 || !strings.Contains(l[0], "not measured") {
		t.Errorf("perf lines = %q", l)
	}
	day, _ := ParseDay("2026-09-24", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	var buf bytes.Buffer
	q := ActivityQuery{Function: "f", Region: "r", LogGroup: "/aws/lambda/f", Day: day, Pattern: regexp.MustCompile(`Copied`), Filter: `"Copied"`}
	if err := RenderActivity(&buf, ActivityReport{Query: q, Scan: scan, Now: day.End}, "json", false); err != nil {
		t.Fatal(err)
	}
	var out struct {
		LogScan struct {
			Performance map[string]any `json:"performance"`
		} `json:"logScan"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	perf := out.LogScan.Performance
	if perf["measured"] != false || perf["durationP50Ms"] != nil || perf["maxMemoryUsedMB"] != nil {
		t.Errorf("unmeasured perf should be null, not 0: %v", perf)
	}
}
