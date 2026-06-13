package debuglog

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestSinkRetainsRecentEntriesAndWraps(t *testing.T) {
	s := NewSink(3)
	for i := 0; i < 5; i++ {
		s.add(Entry{Msg: string(rune('a' + i))})
	}
	got := s.Entries()
	if len(got) != 3 {
		t.Fatalf("want 3 retained, got %d", len(got))
	}
	// Oldest two ("a","b") should have been evicted; oldest-first order kept.
	want := []string{"c", "d", "e"}
	for i, e := range got {
		if e.Msg != want[i] {
			t.Errorf("entry %d: want %q, got %q", i, want[i], e.Msg)
		}
	}
	if s.Dropped() != 2 {
		t.Errorf("want 2 dropped, got %d", s.Dropped())
	}
	if s.Len() != 3 {
		t.Errorf("want Len 3, got %d", s.Len())
	}
}

func TestSinkReset(t *testing.T) {
	s := NewSink(2)
	s.add(Entry{Msg: "x"})
	s.add(Entry{Msg: "y"})
	s.add(Entry{Msg: "z"}) // forces one drop
	s.Reset()
	if s.Len() != 0 || s.Dropped() != 0 || len(s.Entries()) != 0 {
		t.Fatalf("Reset did not clear sink: len=%d dropped=%d", s.Len(), s.Dropped())
	}
}

func TestHandlerCapturesRecordWithAttrs(t *testing.T) {
	s := NewSink(10)
	logger := slog.New(NewHandler(s))
	logger.Info("scanning region", "region", "us-east-1", "service", "ec2")

	entries := s.Entries()
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Msg != "scanning region" {
		t.Errorf("msg: got %q", e.Msg)
	}
	if e.Level != slog.LevelInfo {
		t.Errorf("level: got %v", e.Level)
	}
	if e.Attrs != "region=us-east-1 service=ec2" {
		t.Errorf("attrs: got %q", e.Attrs)
	}
}

func TestHandlerWithAttrsAndGroup(t *testing.T) {
	s := NewSink(10)
	base := NewHandler(s).WithAttrs([]slog.Attr{slog.String("account", "123")})
	grouped := base.WithGroup("aws")
	logger := slog.New(grouped)
	logger.Warn("denied", "region", "eu-west-1")

	e := s.Entries()[0]
	if e.Level != slog.LevelWarn {
		t.Errorf("level: got %v", e.Level)
	}
	// Base attr keeps its key; record attr is prefixed by the active group.
	if e.Attrs != "aws.account=123 aws.region=eu-west-1" {
		t.Errorf("attrs: got %q", e.Attrs)
	}
}

func TestHandlerEnabledAllLevels(t *testing.T) {
	h := NewHandler(NewSink(1))
	for _, lvl := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if !h.Enabled(context.Background(), lvl) {
			t.Errorf("level %v should be enabled", lvl)
		}
	}
}

func TestSinkConcurrentWrites(t *testing.T) {
	s := NewSink(50)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.add(Entry{Time: time.Now(), Msg: "x"})
			}
		}()
	}
	wg.Wait()
	// Bounded regardless of how many writers raced.
	if s.Len() != 50 {
		t.Errorf("want Len 50, got %d", s.Len())
	}
	if got := len(s.Entries()); got != 50 {
		t.Errorf("want 50 entries, got %d", got)
	}
}
