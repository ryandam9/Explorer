package cmd

import "testing"

func TestParseWithin(t *testing.T) {
	cases := map[string]int{
		"":      90, // default
		"90":    90,
		"30d":   30,
		" 14D ": 14,
		"0":     0,
		"48h":   2, // plain Go durations work too
	}
	for in, want := range cases {
		got, err := parseWithin(in)
		if err != nil || got != want {
			t.Errorf("parseWithin(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"-5", "-5d", "soon", "5x"} {
		if _, err := parseWithin(bad); err == nil {
			t.Errorf("parseWithin(%q) should error", bad)
		}
	}
}
