package s3tui

import (
	"testing"
	"time"
)

func TestFormatLocalTime(t *testing.T) {
	// The API hands back UTC; the browser shows the viewer's own zone.
	utc := time.Date(2026, 9, 17, 23, 14, 2, 0, time.UTC)
	want := utc.Local().Format("2006-01-02 15:04:05")
	if got := formatLocalTime(&utc); got != want {
		t.Errorf("formatLocalTime = %q, want the local rendering %q", got, want)
	}

	// A common prefix carries no timestamp: blank, not the zero time.
	if got := formatLocalTime(nil); got != "" {
		t.Errorf("formatLocalTime(nil) = %q, want empty", got)
	}
}

func TestFormatLocalISOCarriesTheOffset(t *testing.T) {
	utc := time.Date(2026, 9, 17, 23, 14, 2, 0, time.UTC)
	got := formatLocalISO(&utc)

	parsed, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("machine output %q does not parse as RFC 3339: %v", got, err)
	}
	if !parsed.Equal(utc) {
		t.Errorf("round-trip changed the instant: %v, want %v", parsed.UTC(), utc)
	}
	if formatLocalISO(nil) != "" {
		t.Errorf("nil timestamp should render empty")
	}
}

func TestParseModSince(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)

	tests := []struct {
		in      string
		want    time.Time
		label   string
		wantErr bool
	}{
		{in: "", want: time.Time{}, label: ""},
		{in: "  ", want: time.Time{}, label: ""},
		{in: "30m", want: now.Add(-30 * time.Minute), label: "30m"},
		{in: "12h", want: now.Add(-12 * time.Hour), label: "12h"},
		{in: "7d", want: now.AddDate(0, 0, -7), label: "7d"},
		{in: "2w", want: now.AddDate(0, 0, -14), label: "2w"},
		{
			in:    "2026-09-01",
			want:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local),
			label: "2026-09-01",
		},
		{
			in:    "2026-09-01 14:30",
			want:  time.Date(2026, 9, 1, 14, 30, 0, 0, time.Local),
			label: "2026-09-01 14:30",
		},
		{in: "yesterday", wantErr: true},
		{in: "7", wantErr: true},
		{in: "-3d", wantErr: true},
		{in: "2026-13-45", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, label, err := parseModSince(tt.in, now)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseModSince(%q) succeeded, want an error naming the accepted forms", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseModSince(%q) errored: %v", tt.in, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("cutoff = %v, want %v", got, tt.want)
			}
			if label != tt.label {
				t.Errorf("label = %q, want %q", label, tt.label)
			}
		})
	}
}

// An absolute date is read in the viewer's zone — "since 2026-09-01" means
// midnight where they are, not midnight UTC.
func TestParseModSinceAbsoluteIsLocal(t *testing.T) {
	got, _, err := parseModSince("2026-09-01", time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if zone, _ := got.Zone(); zone != time.Now().Local().Format("MST") {
		// Comparing the instant is the real check; the zone name varies.
		want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
		if !got.Equal(want) {
			t.Errorf("cutoff = %v, want local midnight %v", got, want)
		}
	}
}

func TestFilterObjectMaps(t *testing.T) {
	iso := func(t time.Time) string { return t.Format(time.RFC3339) }
	now := time.Now()
	rows := []map[string]string{
		{"name": "..", "type": "DIR"},
		{"name": "archive/", "type": "DIR"},
		{"name": "old.log", "type": "FILE", "last_modified_iso": iso(now.AddDate(0, 0, -30))},
		{"name": "new.log", "type": "FILE", "last_modified_iso": iso(now.Add(-time.Hour))},
		{"name": "edge.log", "type": "FILE", "last_modified_iso": iso(now.AddDate(0, 0, -7))},
	}

	// No cutoff: everything, untouched.
	if got := filterObjectMaps(rows, time.Time{}); len(got) != len(rows) {
		t.Errorf("zero cutoff filtered %d of %d rows, want none", len(rows)-len(got), len(rows))
	}

	cutoff := now.AddDate(0, 0, -7).Add(-time.Minute)
	got := filterObjectMaps(rows, cutoff)

	names := map[string]bool{}
	for _, r := range got {
		names[r["name"]] = true
	}
	// Folders and the parent entry survive any cutoff: S3 gives them no
	// timestamp, and hiding them would strand navigation.
	for _, keep := range []string{"..", "archive/", "new.log", "edge.log"} {
		if !names[keep] {
			t.Errorf("%q was filtered out, want it kept", keep)
		}
	}
	if names["old.log"] {
		t.Errorf("old.log is older than the cutoff but survived")
	}
}

// A row we can't read the timestamp of is kept: that's our bug, not evidence
// the object is old, and hiding it would lose data silently.
func TestFilterObjectMapsKeepsUnparseableAndMissing(t *testing.T) {
	rows := []map[string]string{
		{"name": "broken.log", "type": "FILE", "last_modified_iso": "not-a-time"},
		{"name": "missing.log", "type": "FILE"},
	}
	got := filterObjectMaps(rows, time.Now())
	if len(got) != 2 {
		t.Errorf("kept %d of 2 rows with unreadable timestamps, want both", len(got))
	}
}

func TestCountObjectRows(t *testing.T) {
	rows := []map[string]string{
		{"name": "..", "type": "DIR"},
		{"name": "sub/", "type": "DIR"},
		{"name": "a.log", "type": "FILE"},
		{"name": "b.log", "type": "FILE"},
	}
	if got := countObjectRows(rows); got != 2 {
		t.Errorf("countObjectRows = %d, want 2 (folders and .. are not objects)", got)
	}
}
