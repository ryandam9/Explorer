package s3tui

import (
	"fmt"
	"strings"
	"time"
)

// S3 hands every timestamp back in UTC. The rest of the app (emr, glue,
// lambda) renders AWS times in the viewer's own zone, so the object browser
// does too — a "last modified" you have to convert in your head is a worse
// answer than one you can compare with your watch.
const (
	localTimeLayout = "2006-01-02 15:04:05"
	// dateOnlyLayout and dateTimeLayout are the absolute forms the M prompt
	// accepts, both read in local time.
	dateOnlyLayout = "2006-01-02"
	dateTimeLayout = "2006-01-02 15:04"
)

// formatLocalTime renders an AWS timestamp in the local zone for display. A
// nil timestamp (the API omits it for a common prefix) renders empty rather
// than as the zero time, so "no date" never reads as 1 January year 1.
func formatLocalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format(localTimeLayout)
}

// formatLocalISO renders the same instant as RFC 3339 with the local offset
// ("2026-09-17T09:14:02+10:00"). It is what machine-readable output carries:
// a bare local timestamp can't be interpreted without knowing the writer's
// zone, and switching the export to local time without the offset would make
// exported data ambiguous.
func formatLocalISO(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Local().Format(time.RFC3339)
}

// parseModSince reads the "modified since" prompt: a relative age ("30m",
// "12h", "7d", "2w") or an absolute local date ("2026-09-01") or date and time
// ("2026-09-01 14:30"). It returns the cutoff, the label to show for it, and
// an error explaining the accepted forms. An empty input clears the filter,
// reported as a zero cutoff.
func parseModSince(s string, now time.Time) (time.Time, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, "", nil
	}

	if d, ok := parseAge(s); ok {
		if d <= 0 {
			return time.Time{}, "", fmt.Errorf("%q must be a positive age", s)
		}
		return now.Add(-d), s, nil
	}

	for _, layout := range []string{dateTimeLayout, dateOnlyLayout} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, t.Format(layout), nil
		}
	}

	return time.Time{}, "", fmt.Errorf("use an age (30m, 12h, 7d, 2w) or a local date (2026-09-01, 2026-09-01 14:30)")
}

// parseAge parses a relative age. Days and weeks are handled here because
// time.ParseDuration stops at hours.
func parseAge(s string) (time.Duration, bool) {
	mult := time.Duration(0)
	switch {
	case strings.HasSuffix(s, "d"):
		mult = 24 * time.Hour
	case strings.HasSuffix(s, "w"):
		mult = 7 * 24 * time.Hour
	default:
		d, err := time.ParseDuration(s)
		return d, err == nil
	}
	n, err := time.ParseDuration(strings.TrimSuffix(s, s[len(s)-1:]) + "h")
	if err != nil {
		return 0, false
	}
	return n / time.Hour * mult, true
}

// filterObjectMaps keeps the rows at or after cutoff. A zero cutoff means no
// filter and the rows pass through untouched.
//
// Three kinds of row are kept whatever the cutoff, because hiding them would
// mislead rather than narrow:
//   - the ".." parent entry and folders (common prefixes), which S3 gives no
//     timestamp at all — filtering them out would strand you in a directory
//     you can't navigate out of, and would suggest the folder holds nothing
//     recent when nothing was ever checked;
//   - a row whose stored timestamp won't parse, which is a bug on our side,
//     not evidence the object is old.
func filterObjectMaps(rows []map[string]string, cutoff time.Time) []map[string]string {
	if cutoff.IsZero() {
		return rows
	}
	out := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		if r["name"] == ".." || r["type"] != "FILE" {
			out = append(out, r)
			continue
		}
		iso := r["last_modified_iso"]
		if iso == "" {
			out = append(out, r)
			continue
		}
		t, err := time.Parse(time.RFC3339, iso)
		if err != nil || !t.Before(cutoff) {
			out = append(out, r)
		}
	}
	return out
}

// countObjectRows counts the FILE rows in a listing — what "N of M" means to
// the reader, who is not counting the folders or the ".." entry.
func countObjectRows(rows []map[string]string) int {
	n := 0
	for _, r := range rows {
		if r["type"] == "FILE" {
			n++
		}
	}
	return n
}
