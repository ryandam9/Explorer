package lambdatui

import (
	"bytes"
	"math"
	"strings"
	"testing"
	"time"
)

func TestParseDayRanges(t *testing.T) {
	syd, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Skip("no tz database:", err)
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, syd)

	d, err := ParseDay("2026-09-20..2026-09-24", now, syd)
	if err != nil || d.Days() != 5 || d.Spec() != "2026-09-20..2026-09-24" || d.Hours() != 120 {
		t.Fatalf("A..B = %+v days=%d hours=%d err=%v", d, d.Days(), d.Hours(), err)
	}
	if got := d.Label(); got != "Sun 2026-09-20 → Thu 2026-09-24 (5 days, AEST, UTC+10:00)" {
		t.Errorf("label = %q", got)
	}
	// A range steps by its own length.
	if next := d.Shift(1); next.Spec() != "2026-09-25..2026-09-29" {
		t.Errorf("shift = %s", next.Spec())
	}

	// 7d: the last seven days, today included.
	d, err = ParseDay("7d", now, syd)
	if err != nil || d.Spec() != "2026-09-30..2026-10-06" || !d.InProgress(now) {
		t.Errorf("7d = %s err=%v", d.Spec(), err)
	}
	// Keywords work on either side; a one-day range is a plain day.
	if d, _ := ParseDay("yesterday..today", now, syd); d.Spec() != "2026-10-05..2026-10-06" {
		t.Errorf("yesterday..today = %s", d.Spec())
	}
	if d, _ := ParseDay("2026-09-24..2026-09-24", now, syd); d.Spec() != "2026-09-24" || d.Days() != 1 {
		t.Errorf("a one-day range should be a day: %s", d.Spec())
	}

	// DST starts in Sydney on 2026-10-04: that window is an hour short.
	if d, _ := ParseDay("2026-10-03..2026-10-05", now, syd); d.Hours() != 71 {
		t.Errorf("DST window hours = %d, want 71", d.Hours())
	}

	for _, bad := range []string{"2026-09-24..2026-09-20", "40d", "0d", "2026-08-01..2026-09-24", "2026-10-06..2026-10-07", "2026-09-24..nope"} {
		if _, err := ParseDay(bad, now, syd); err == nil {
			t.Errorf("ParseDay(%q) should fail", bad)
		}
	}
}

func TestDailyTotals(t *testing.T) {
	day, _ := ParseDay("2026-09-20..2026-09-22", time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), time.UTC)
	inv := metricSeries{
		Timestamps: []time.Time{day.Start.Add(2 * time.Hour), day.Start.Add(3 * time.Hour), day.Start.Add(50 * time.Hour)},
		Values:     []float64{3, 4, 9},
	}
	st := buildInvocationStats(day, inv, metricSeries{}, metricSeries{})
	daily := st.Daily()
	if len(daily) != 3 || daily[0].Invocations != 7 || !math.IsNaN(daily[1].Invocations) || daily[2].Invocations != 9 {
		t.Fatalf("daily = %+v", daily)
	}
	if p, ok := st.PeakDay(); !ok || p.Date != "2026-09-22" {
		t.Errorf("peak day = %+v", p)
	}

	// The CLI table prints one row per day for a range.
	var buf bytes.Buffer
	r := ActivityReport{Query: ActivityQuery{Function: "f", Region: "r", Day: day}, Stats: st, Now: day.End}
	if err := RenderActivity(&buf, r, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "DAY") || !strings.Contains(out, "Sun 2026-09-20  7") || !strings.Contains(out, "Mon 2026-09-21  -") ||
		strings.Contains(out, "HOUR") {
		t.Errorf("range table:\n%s", out)
	}
	if got := matchTime(day.Start.Add(26*time.Hour+time.Millisecond), day); got != "09-21 02:00:00.001" {
		t.Errorf("a range's times carry the date: %q", got)
	}
}
