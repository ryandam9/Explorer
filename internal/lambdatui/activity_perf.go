package lambdatui

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Performance summary: the platform writes one REPORT line per invocation
// (duration, billed duration, memory size, max memory used, and an init
// duration on a cold start). The log scan already reads them, so it folds
// each into PerfStats — p50/p95/max duration against the timeout, memory
// headroom, cold starts and an estimated compute cost — with no extra AWS
// call. Pure, so it is fixture-tested.

// On-demand Lambda list prices (us-east-1; most regions match, some differ):
// per GB-second of billed duration by architecture, and per request.
const (
	priceGBSecondX86   = 0.0000166667
	priceGBSecondArm64 = 0.0000133334
	pricePerRequest    = 0.20 / 1_000_000
)

// PerfStats accumulates the REPORT lines of a scan.
type PerfStats struct {
	Count       int
	durations   []float64 // ms, one per REPORT
	maxMemory   []float64 // MB used, one per REPORT
	memSizes    map[float64]bool
	GBSeconds   float64 // Σ billed duration × memory size
	ColdStarts  int
	initTotalMs float64
	InitMaxMs   float64
	Timeouts    int // REPORT status timeout
}

func (p *PerfStats) add(r InvocationReport) {
	if p.memSizes == nil {
		p.memSizes = map[float64]bool{}
	}
	p.Count++
	p.durations = append(p.durations, r.DurationMs)
	p.maxMemory = append(p.maxMemory, r.MaxMemoryMB)
	if r.MemorySizeMB > 0 {
		p.memSizes[r.MemorySizeMB] = true
		p.GBSeconds += r.BilledMs / 1000 * r.MemorySizeMB / 1024
	}
	if r.ColdStart {
		p.ColdStarts++
		p.initTotalMs += r.InitMs
		p.InitMaxMs = math.Max(p.InitMaxMs, r.InitMs)
	}
	if r.Status == "timeout" {
		p.Timeouts++
	}
}

// percentile is the nearest-rank percentile (q in 0..100) of vals.
func percentile(vals []float64, q float64) float64 {
	if len(vals) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	rank := int(math.Ceil(q / 100 * float64(len(s))))
	return s[min(max(rank-1, 0), len(s)-1)]
}

// DurationP returns the q-th percentile duration in ms (NaN with no reports).
func (p *PerfStats) DurationP(q float64) float64 { return percentile(p.durations, q) }

// MaxDurationMs / MaxMemoryMB are the day's worst run (NaN with no reports).
func (p *PerfStats) MaxDurationMs() float64 { return percentile(p.durations, 100) }
func (p *PerfStats) MaxMemoryMB() float64   { return percentile(p.maxMemory, 100) }

// InitAvgMs is the mean cold-start init duration (NaN with no cold start).
func (p *PerfStats) InitAvgMs() float64 {
	if p.ColdStarts == 0 {
		return math.NaN()
	}
	return p.initTotalMs / float64(p.ColdStarts)
}

// MemorySizes lists the configured sizes seen (more than one when the
// function was reconfigured during the window).
func (p *PerfStats) MemorySizes() []float64 {
	out := make([]float64, 0, len(p.memSizes))
	for s := range p.memSizes {
		out = append(out, s)
	}
	sort.Float64s(out)
	return out
}

// EstimatedCost is the compute + request charge at on-demand list prices,
// before the free tier. arch is "arm64" or anything else for x86_64.
func (p *PerfStats) EstimatedCost(arch string) float64 {
	rate := priceGBSecondX86
	if arch == "arm64" {
		rate = priceGBSecondArm64
	}
	return p.GBSeconds*rate + float64(p.Count)*pricePerRequest
}

// PerfKnown reports whether the scan's REPORT lines are a fair sample of the
// window: a server-side filter usually hides them (unless it matches them),
// so with a filter and no REPORT line the figures are "not measured", not 0.
func (s *LogScan) PerfKnown() bool {
	return s.Perf.Count > 0 || s.Filter == ""
}

// PerfPartial reports whether the figures cover only part of the window (the
// scan stopped early, failed, or a filter chose which REPORT lines were read).
func (s *LogScan) PerfPartial() bool {
	return s.StopReason != "" || s.Err != nil || s.Filter != ""
}

// perfLines renders the summary as two lines — duration, then memory/cold
// starts/cost — against the function's timeout and memory size when known.
// It returns a single explanatory line when nothing was measured.
func perfLines(s *LogScan, timeoutSec int32, arch string) []string {
	p := &s.Perf
	switch {
	case !s.PerfKnown():
		return []string{"performance not measured — the server filter hides the REPORT lines"}
	case p.Count == 0:
		if s.Done {
			return []string{"performance: no REPORT lines in the window read"}
		}
		return nil
	}
	scope := fmt.Sprintf("%d run(s)", p.Count)
	if s.PerfPartial() {
		scope += " read (partial)"
	}
	dur := fmt.Sprintf("duration  p50 %s · p95 %s · max %s", formatMs(p.DurationP(50)), formatMs(p.DurationP(95)), formatMs(p.MaxDurationMs()))
	if timeoutSec > 0 {
		dur += fmt.Sprintf(" — %.0f%% of the %ds timeout", 100*p.MaxDurationMs()/(float64(timeoutSec)*1000), timeoutSec)
	}
	if p.Timeouts > 0 {
		dur += fmt.Sprintf(" · %d timed out", p.Timeouts)
	}
	dur += "  (" + scope + ")"

	var mem string
	if sizes := p.MemorySizes(); len(sizes) > 0 {
		size := sizes[len(sizes)-1]
		mem = fmt.Sprintf("memory    max %.0f of %.0f MB (%.0f%%)", p.MaxMemoryMB(), size, 100*p.MaxMemoryMB()/size)
		if len(sizes) > 1 {
			parts := make([]string, len(sizes))
			for i, v := range sizes {
				parts[i] = fmt.Sprintf("%.0f", v)
			}
			mem += " · size changed during the window (" + strings.Join(parts, "/") + " MB)"
		}
	} else {
		mem = fmt.Sprintf("memory    max %.0f MB", p.MaxMemoryMB())
	}
	if p.ColdStarts > 0 {
		mem += fmt.Sprintf(" · %d cold start(s), init avg %s / max %s", p.ColdStarts, formatMs(p.InitAvgMs()), formatMs(p.InitMaxMs))
	} else {
		mem += " · no cold starts"
	}
	mem += fmt.Sprintf(" · est. %s", formatUSD(p.EstimatedCost(arch)))
	return []string{dur, mem}
}

// formatUSD renders a small dollar amount with enough precision to be
// non-zero ($0.000042 rather than $0.00).
func formatUSD(v float64) string {
	switch {
	case v == 0:
		return "$0"
	case v < 0.01:
		return fmt.Sprintf("$%.6f", v)
	default:
		return fmt.Sprintf("$%.2f", v)
	}
}

// primaryArch is a function's architecture for pricing ("x86_64" when none is
// listed, Lambda's default).
func primaryArch(archs []string) string {
	if len(archs) > 0 && archs[0] != "" {
		return archs[0]
	}
	return "x86_64"
}
