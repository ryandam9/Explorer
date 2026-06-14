package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

func TestCoverageBanner(t *testing.T) {
	base := tuiModel{width: 200, coverageAdvisory: true, coverageTagSweep: true, coverageMissing: 3}

	if b := base.coverageBanner(); !strings.Contains(b, "Coverage") || !strings.Contains(b, "3") {
		t.Errorf("banner should mention coverage and the count, got %q", b)
	}

	off := base
	off.coverageAdvisory = false
	if off.coverageBanner() != "" {
		t.Error("banner should be empty when the advisory is disabled")
	}

	none := base
	none.coverageMissing = 0
	if none.coverageBanner() != "" {
		t.Error("banner should be empty when no common service is missing")
	}

	typedOnly := base
	typedOnly.coverageTagSweep = false
	if !strings.Contains(typedOnly.coverageBanner(), "typed-only") {
		t.Errorf("typed-only banner should mention the flag, got %q", typedOnly.coverageBanner())
	}
}

func TestCoverageBannerReservesTableLine(t *testing.T) {
	on := tuiModel{height: 40, width: 200, coverageAdvisory: true, coverageMissing: 2}
	off := on
	off.coverageMissing = 0
	if on.tableHeight() != off.tableHeight()-1 {
		t.Errorf("the banner should cost exactly one table line: on=%d off=%d",
			on.tableHeight(), off.tableHeight())
	}
}

func TestWithCoverageAdvisoryOption(t *testing.T) {
	m := NewModelWithSeed(context.Background(), nil, "", &config.Config{}, nil,
		WithCoverageAdvisory(false)).(tuiModel)
	if !m.coverageAdvisory {
		t.Error("WithCoverageAdvisory should turn the advisory on")
	}
	if m.coverageTagSweep {
		t.Error("coverageTagSweep should be false when --typed-only")
	}
	// Default construction leaves the advisory off.
	plain := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	if plain.coverageAdvisory {
		t.Error("the plain TUI should not show the coverage advisory")
	}
}

func TestBuildSwitcherFormBuilds(t *testing.T) {
	// Long region lists were overflowing the modal with no scroll; the form is
	// built with bounded, scrollable lists. At minimum it must build cleanly
	// across terminal sizes.
	for _, h := range []int{18, 40, 60} {
		m := newTestModel(t, 120, h)
		if f := m.buildSwitcherForm(); f == nil {
			t.Fatalf("buildSwitcherForm returned nil at height %d", h)
		}
	}
}
