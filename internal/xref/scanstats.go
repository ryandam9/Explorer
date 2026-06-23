package xref

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ScanStats accumulates per-service wall-clock time across the (concurrent)
// region fan-out, so --debug-scan can show where a slow scan spends its time
// (#394). It is concurrency-safe; a nil *ScanStats is a no-op so the timing
// hooks can be left in place unconditionally.
type ScanStats struct {
	mu  sync.Mutex
	dur map[string]time.Duration
}

func (s *ScanStats) add(service string, d time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dur == nil {
		s.dur = map[string]time.Duration{}
	}
	s.dur[service] += d
}

// timed runs fn, attributing its wall time to service, and returns fn's edges.
func (s *ScanStats) timed(service string, fn func() []Edge) []Edge {
	start := time.Now()
	edges := fn()
	s.add(service, time.Since(start))
	return edges
}

// Lines returns the per-service timings, slowest first, as display strings.
func (s *ScanStats) Lines() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	type kv struct {
		svc string
		d   time.Duration
	}
	rows := make([]kv, 0, len(s.dur))
	for svc, d := range s.dur {
		rows = append(rows, kv{svc, d})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].d != rows[j].d {
			return rows[i].d > rows[j].d
		}
		return rows[i].svc < rows[j].svc
	})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%-14s %s", r.svc, r.d.Round(time.Millisecond)))
	}
	return out
}
