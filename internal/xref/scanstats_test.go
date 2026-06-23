package xref

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestScanStats_TimedAndOrdered(t *testing.T) {
	s := &ScanStats{}
	s.add("kms", 30*time.Millisecond)
	s.add("kms", 20*time.Millisecond) // accumulates → 50ms
	s.add("s3", 100*time.Millisecond)
	s.add("lambda", 10*time.Millisecond)

	lines := s.Lines()
	if len(lines) != 3 {
		t.Fatalf("want 3 service lines, got %d: %v", len(lines), lines)
	}
	// Slowest first: s3 (100) > kms (50) > lambda (10).
	if !strings.HasPrefix(lines[0], "s3") || !strings.HasPrefix(lines[1], "kms") || !strings.HasPrefix(lines[2], "lambda") {
		t.Errorf("lines not ordered slowest-first: %v", lines)
	}

	// timed attributes wall time and returns the edges.
	got := s.timed("ec2", func() []Edge { return []Edge{{Target: "x"}} })
	if len(got) != 1 {
		t.Errorf("timed should return fn's edges, got %v", got)
	}
}

func TestScanStats_NilIsNoOp(t *testing.T) {
	var s *ScanStats
	s.add("kms", time.Second) // must not panic
	if got := s.timed("kms", func() []Edge { return nil }); got != nil {
		t.Errorf("nil stats timed should still run fn and return its result")
	}
	if s.Lines() != nil {
		t.Errorf("nil stats Lines should be nil")
	}
}

func TestScanStats_ConcurrentAddIsSafe(t *testing.T) {
	s := &ScanStats{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.add("kms", time.Millisecond)
		}()
	}
	wg.Wait()
	if len(s.Lines()) != 1 {
		t.Errorf("want 1 aggregated service, got %v", s.Lines())
	}
}
