package cwtui

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// queryWindow is the time range an events query covers.
//
// The window normally counts back from now ("last 7d"). A single stream whose
// last event is older than that would always come back empty — and no preset
// is wide enough to reach a stream from last month — so for a stream the
// window moves back to end at the stream's own last event instead: "the 7d
// before this stream's last event". It stays open-ended (no EndTime) because
// LastEventTimestamp is updated eventually (AWS: typically within an hour),
// so events newer than it may already exist.
type queryWindow struct {
	lookback time.Duration
	start    int64 // FilterLogEvents StartTime, epoch ms (inclusive)
	// anchor is the stream's last event (epoch ms) when the window was moved
	// back to it; 0 when the window counts back from now.
	anchor int64
}

// windowFor returns the query window for lookback at now. stream is the
// selected stream, or nil for a group-wide query, which always counts back
// from now: there the window is what bounds how much data a search scans.
func windowFor(lookback time.Duration, now time.Time, stream *types.LogStream) queryWindow {
	w := queryWindow{lookback: lookback, start: now.Add(-lookback).UnixMilli()}
	if stream == nil {
		return w
	}
	last := aws.ToInt64(stream.LastEventTimestamp)
	if last <= 0 || last >= w.start {
		// No events, or the last event is inside the window already.
		return w
	}
	w.anchor = last
	w.start = last - lookback.Milliseconds()
	return w
}

// anchored reports whether the window was moved back to the stream's last event.
func (w queryWindow) anchored() bool { return w.anchor != 0 }

// label describes the window for the events panel header, e.g. "last 7d" or
// "7d before this stream's last event (2026-09-03 14:22)".
func (w queryWindow) label() string {
	if w.anchored() {
		return fmt.Sprintf("%s before this stream's last event (%s)", formatLookback(w.lookback), formatMillis(w.anchor))
	}
	return "last " + formatLookback(w.lookback)
}

// short is the compact form for the status bar and toasts.
func (w queryWindow) short() string {
	if w.anchored() {
		return formatLookback(w.lookback) + " to " + formatMillis(w.anchor)
	}
	return formatLookback(w.lookback)
}

// emptyNote explains an empty result with the actual range searched, so "no
// events" can't be mistaken for "this stream is empty".
func (w queryWindow) emptyNote() string {
	if w.anchored() {
		return fmt.Sprintf("No matching log events in the %s before this stream's last event (%s to %s).",
			formatLookback(w.lookback), formatMillis(w.start), formatMillis(w.anchor))
	}
	return fmt.Sprintf("No matching log events found in this window (since %s).", formatMillis(w.start))
}

// formatMillis renders an epoch-ms timestamp in local time, like the streams
// list does.
func formatMillis(ms int64) string {
	return time.UnixMilli(ms).Format("2006-01-02 15:04")
}
