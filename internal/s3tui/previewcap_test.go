package s3tui

import "testing"

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"10MB", 10 << 20, true},
		{"512KB", 512 << 10, true},
		{"1GB", 1 << 30, true},
		{"  2 mb ", 2 << 20, true},
		{"1.5MB", 1572864, true},
		{"2048", 2048, true},  // bare bytes
		{"4096B", 4096, true}, // explicit bytes suffix
		{"", 0, false},        // empty
		{"abc", 0, false},     // garbage
		{"MB", 0, false},      // unit with no number
		{"-5MB", 0, false},    // negative
		{"0", 0, false},       // zero is not a usable size
	}
	for _, c := range cases {
		got, ok := parseByteSize(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseByteSize(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParsePreviewCapDefaultsAndClamps(t *testing.T) {
	if got := parsePreviewCap(""); got != textPreviewCap {
		t.Errorf("empty should default to %d, got %d", textPreviewCap, got)
	}
	if got := parsePreviewCap("nonsense"); got != textPreviewCap {
		t.Errorf("garbage should default to %d, got %d", textPreviewCap, got)
	}
	// Below the floor clamps up.
	if got := parsePreviewCap("1KB"); got != minTextPreviewCap {
		t.Errorf("1KB should clamp to %d, got %d", minTextPreviewCap, got)
	}
	// Above the ceiling clamps down.
	if got := parsePreviewCap("500MB"); got != maxTextPreviewCap {
		t.Errorf("500MB should clamp to %d, got %d", maxTextPreviewCap, got)
	}
	// A sane value passes through.
	if got := parsePreviewCap("2MB"); got != 2<<20 {
		t.Errorf("2MB should pass through, got %d", got)
	}
}
