package sparkline

import (
	"math"
	"testing"
)

func TestRender_Golden(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		want string
	}{
		{"empty", nil, ""},
		{"ascending", []float64{0, 1, 2, 3, 4, 5, 6, 7}, "▁▂▃▄▅▆▇█"},
		{"flat zero", []float64{0, 0, 0}, "▁▁▁"},
		{"flat nonzero rides mid", []float64{5, 5, 5}, "▅▅▅"},
		{"two-level", []float64{0, 10}, "▁█"},
		{"all missing", []float64{math.NaN(), math.NaN()}, ""},
		{"gap in middle", []float64{0, math.NaN(), 7}, "▁ █"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Render(c.in); got != c.want {
				t.Errorf("Render(%v) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestRender_LengthMatchesInput(t *testing.T) {
	in := []float64{1, 2, math.NaN(), 4, 5}
	got := []rune(Render(in))
	if len(got) != len(in) {
		t.Errorf("rendered %d runes, want %d (one per datapoint)", len(got), len(in))
	}
}

func TestSummarize(t *testing.T) {
	s, ok := Summarize([]float64{3, 91, math.NaN(), 12})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if s.Now != 12 {
		t.Errorf("Now = %v, want 12 (latest real datapoint)", s.Now)
	}
	if s.Max != 91 {
		t.Errorf("Max = %v, want 91", s.Max)
	}
	if s.Min != 3 {
		t.Errorf("Min = %v, want 3", s.Min)
	}

	if _, ok := Summarize([]float64{math.NaN()}); ok {
		t.Error("Summarize of all-NaN should report ok=false")
	}
	if _, ok := Summarize(nil); ok {
		t.Error("Summarize of empty should report ok=false")
	}
}

func TestSummarize_NowSkipsTrailingGaps(t *testing.T) {
	s, ok := Summarize([]float64{5, 9, math.NaN(), math.NaN()})
	if !ok || s.Now != 9 {
		t.Errorf("Now = %v (ok=%v), want 9 (latest real datapoint before trailing gaps)", s.Now, ok)
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{12, "%", "12%"},
		{0, "", "0"},
		{12.345, "", "12.35"},
		{15000, "", "15.0k"},
		{2_500_000, "", "2.5M"},
		{math.NaN(), "%", "—"},
	}
	for _, c := range cases {
		if got := FormatValue(c.v, c.unit); got != c.want {
			t.Errorf("FormatValue(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}
