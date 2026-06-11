package ui

import (
	"math"
	"testing"
)

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		in      string
		r, g, b int
		ok      bool
	}{
		{"#feca00", 0xfe, 0xca, 0x00, true},
		{"#FECA00", 0xfe, 0xca, 0x00, true},
		{"#fff", 0xff, 0xff, 0xff, true},
		{" #000100 ", 0x00, 0x01, 0x00, true},
		{"feca00", 0, 0, 0, false},
		{"#feca0", 0, 0, 0, false},
		{"#gggggg", 0, 0, 0, false},
		{"", 0, 0, 0, false},
		{"red", 0, 0, 0, false},
	}
	for _, tt := range tests {
		r, g, b, ok := parseHexColor(tt.in)
		if ok != tt.ok || r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("parseHexColor(%q) = (%d,%d,%d,%v), want (%d,%d,%d,%v)",
				tt.in, r, g, b, ok, tt.r, tt.g, tt.b, tt.ok)
		}
	}
}

func TestHexHSLKnownValues(t *testing.T) {
	tests := []struct {
		hex     string
		h, s, l float64
	}{
		{"#ff0000", 0, 100, 50},
		{"#00ff00", 120, 100, 50},
		{"#0000ff", 240, 100, 50},
		{"#ffffff", 0, 0, 100},
		{"#000000", 0, 0, 0},
		{"#808080", 0, 0, 50.2},
	}
	for _, tt := range tests {
		h, s, l, ok := hexToHSL(tt.hex)
		if !ok {
			t.Errorf("hexToHSL(%q) not ok", tt.hex)
			continue
		}
		if math.Abs(h-tt.h) > 1 || math.Abs(s-tt.s) > 1 || math.Abs(l-tt.l) > 1 {
			t.Errorf("hexToHSL(%q) = (%.1f,%.1f,%.1f), want ≈(%.1f,%.1f,%.1f)",
				tt.hex, h, s, l, tt.h, tt.s, tt.l)
		}
	}
}

// Round-tripping hex → HSL → hex must reproduce the color (within rounding).
func TestHexHSLRoundTrip(t *testing.T) {
	colors := []string{
		"#feca00", "#d36328", "#cb0300", "#b4b9b3", "#424847",
		"#e7aa01", "#73481b", "#ff5555", "#ffaa00", "#34e0a1",
		"#000000", "#ffffff", "#123456",
	}
	for _, hex := range colors {
		h, s, l, ok := hexToHSL(hex)
		if !ok {
			t.Fatalf("hexToHSL(%q) not ok", hex)
		}
		got := hslToHex(h, s, l)
		r1, g1, b1, _ := parseHexColor(hex)
		r2, g2, b2, _ := parseHexColor(got)
		if abs(r1-r2) > 1 || abs(g1-g2) > 1 || abs(b1-b2) > 1 {
			t.Errorf("round trip %q → (%.2f,%.2f,%.2f) → %q drifted", hex, h, s, l, got)
		}
	}
}

func TestHSLToHexClampsAndWraps(t *testing.T) {
	if got := hslToHex(360, 100, 50); got != "#ff0000" {
		t.Errorf("hue 360 should wrap to red, got %q", got)
	}
	if got := hslToHex(0, -10, 200); got != "#ffffff" {
		t.Errorf("clamped (s=-10, l=200) should be white, got %q", got)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
