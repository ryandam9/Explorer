package cmd

import (
	"testing"
	"time"
)

func TestParseSince_Empty(t *testing.T) {
	got, err := parseSince("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Errorf("parseSince(\"\") = %v, want zero time", got)
	}
}

func TestParseSince_Days(t *testing.T) {
	for _, in := range []string{"7", "7d", "7D"} {
		got, err := parseSince(in)
		if err != nil {
			t.Fatalf("parseSince(%q): %v", in, err)
		}
		want := time.Now().AddDate(0, 0, -7)
		if diff := got.Sub(want); diff < -time.Minute || diff > time.Minute {
			t.Errorf("parseSince(%q) = %v, want ≈ %v", in, got, want)
		}
	}
}

func TestParseSince_Duration(t *testing.T) {
	got, err := parseSince("36h")
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().Add(-36 * time.Hour)
	if diff := got.Sub(want); diff < -time.Minute || diff > time.Minute {
		t.Errorf("parseSince(36h) = %v, want ≈ %v", got, want)
	}
}

func TestParseSince_Invalid(t *testing.T) {
	for _, in := range []string{"yesterday", "-3d", "-12h"} {
		if _, err := parseSince(in); err == nil {
			t.Errorf("parseSince(%q): expected error", in)
		}
	}
}
