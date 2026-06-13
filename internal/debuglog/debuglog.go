// Package debuglog provides an in-memory, ring-buffered slog.Handler that the
// TUIs use to show a live "what is the tool doing right now" debug pane.
//
// The TUIs render with Bubble Tea's alternate screen buffer, so structured
// logs can never be written to the terminal directly — a stray line is painted
// over the interface and corrupts it (see cmd.SilenceScanLogs). Instead, scan
// logs are captured here, in a bounded ring buffer, and a screen reads them on
// demand to render a scrollable debug overlay. Nothing in this package paints
// to the screen, so it is always safe to attach.
package debuglog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// DefaultCapacity is the number of recent log records kept by the shared sink.
// A scan of a busy account emits a few hundred lines; this keeps the most
// recent activity without growing without bound over a long-running session.
const DefaultCapacity = 1000

// Entry is a single captured log record, pre-rendered into the pieces a debug
// pane needs to display it.
type Entry struct {
	Time  time.Time
	Level slog.Level
	Msg   string
	// Attrs is the record's key=value attributes joined into one line, in the
	// order they were logged (base handler attrs first, then record attrs).
	Attrs string
}

// Sink is a thread-safe ring buffer of recent log entries. The engine scans
// concurrently across goroutines, so writes arrive from many goroutines at
// once; reads come from the UI thread when a debug pane is opened.
type Sink struct {
	mu      sync.Mutex
	buf     []Entry
	cap     int
	start   int // index of the oldest entry when the buffer has wrapped
	count   int
	dropped int // entries evicted since the last Snapshot, for a "…(N more)" hint
}

// NewSink returns an empty sink that retains the most recent capacity entries.
func NewSink(capacity int) *Sink {
	if capacity < 1 {
		capacity = 1
	}
	return &Sink{buf: make([]Entry, capacity), cap: capacity}
}

// Default is the process-wide sink the TUIs read from. cmd.SilenceScanLogs
// attaches a handler over it so scan activity is captured without touching the
// screen.
var Default = NewSink(DefaultCapacity)

func (s *Sink) add(e Entry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count < s.cap {
		s.buf[(s.start+s.count)%s.cap] = e
		s.count++
		return
	}
	// Full: overwrite the oldest entry and advance the window.
	s.buf[s.start] = e
	s.start = (s.start + 1) % s.cap
	s.dropped++
}

// Entries returns a copy of the retained entries, oldest first. Snapshotting
// under the lock keeps the caller (the UI thread) decoupled from concurrent
// writers.
func (s *Sink) Entries() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, s.count)
	for i := 0; i < s.count; i++ {
		out[i] = s.buf[(s.start+i)%s.cap]
	}
	return out
}

// Len reports how many entries are currently retained.
func (s *Sink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// Dropped reports how many entries have been evicted to make room since the
// buffer first filled, so a pane can show that older lines scrolled off.
func (s *Sink) Dropped() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}

// Reset clears the buffer. Used at the start of a fresh scan so the pane shows
// only the current run's activity.
func (s *Sink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.start = 0
	s.count = 0
	s.dropped = 0
}

// handler is a slog.Handler that appends every record to a Sink. It captures
// all levels (Enabled always returns true); a debug pane is exactly where the
// low-level detail is wanted.
type handler struct {
	sink  *Sink
	attrs []slog.Attr // accumulated via WithAttrs, rendered ahead of record attrs
	group string      // accumulated via WithGroup, prefixed onto attr keys
}

// NewHandler returns a slog.Handler that records into sink.
func NewHandler(sink *Sink) slog.Handler {
	return &handler{sink: sink}
}

func (h *handler) Enabled(context.Context, slog.Level) bool { return true }

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	var parts []string
	for _, a := range h.attrs {
		parts = appendAttr(parts, h.group, a)
	}
	r.Attrs(func(a slog.Attr) bool {
		parts = appendAttr(parts, h.group, a)
		return true
	})
	h.sink.add(Entry{
		Time:  r.Time,
		Level: r.Level,
		Msg:   r.Message,
		Attrs: strings.Join(parts, " "),
	})
	return nil
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	nh := *h
	nh.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &nh
}

func (h *handler) WithGroup(name string) slog.Handler {
	nh := *h
	if name == "" {
		return &nh
	}
	if nh.group == "" {
		nh.group = name
	} else {
		nh.group = nh.group + "." + name
	}
	return &nh
}

func appendAttr(parts []string, group string, a slog.Attr) []string {
	if a.Equal(slog.Attr{}) {
		return parts
	}
	key := a.Key
	if group != "" {
		key = group + "." + key
	}
	return append(parts, fmt.Sprintf("%s=%v", key, a.Value.Any()))
}
