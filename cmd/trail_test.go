package cmd

import (
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/trail"
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

// resetTrailFilterFlags clears the package-level filter flags buildTrailFilter
// reads, so each case starts from a clean slate.
func resetTrailFilterFlags() {
	trailBy, trailEvent, trailSource = "", "", ""
}

func TestBuildTrailFilter_AccountWide(t *testing.T) {
	resetTrailFilterFlags()
	f, scope, err := buildTrailFilter(nil)
	if err != nil {
		t.Fatal(err)
	}
	if f != (trail.Filter{}) {
		t.Errorf("expected the zero filter for account-wide, got %+v", f)
	}
	if scope != "account-wide activity" {
		t.Errorf("scope = %q", scope)
	}
}

func TestBuildTrailFilter_Pivots(t *testing.T) {
	resetTrailFilterFlags()
	defer resetTrailFilterFlags()

	t.Run("resource ARN is reduced", func(t *testing.T) {
		resetTrailFilterFlags()
		f, scope, err := buildTrailFilter([]string{"arn:aws:ec2:us-east-1:123456789012:instance/i-0abc"})
		if err != nil {
			t.Fatal(err)
		}
		if f.ResourceName != "i-0abc" {
			t.Errorf("ResourceName = %q, want i-0abc", f.ResourceName)
		}
		if scope != "resource i-0abc" {
			t.Errorf("scope = %q", scope)
		}
	})

	t.Run("--by sets principal", func(t *testing.T) {
		resetTrailFilterFlags()
		trailBy = "alice"
		f, _, err := buildTrailFilter(nil)
		if err != nil || f.Principal != "alice" {
			t.Errorf("Principal = %q, err = %v", f.Principal, err)
		}
	})
}

func TestBuildTrailFilter_RejectsMultipleFilters(t *testing.T) {
	resetTrailFilterFlags()
	defer resetTrailFilterFlags()
	trailBy = "alice"
	if _, _, err := buildTrailFilter([]string{"i-0abc"}); err == nil {
		t.Error("expected an error when a resource and --by are both set")
	}

	resetTrailFilterFlags()
	trailEvent, trailSource = "RunInstances", "ec2.amazonaws.com"
	if _, _, err := buildTrailFilter(nil); err == nil {
		t.Error("expected an error when --event and --source are both set")
	}
}
