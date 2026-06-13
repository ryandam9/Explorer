package ui

import (
	"strings"
	"testing"
)

func TestRegionBadge(t *testing.T) {
	t.Run("all-regions shows nothing", func(t *testing.T) {
		if got := RegionBadge([]string{"us-east-1"}, true); got != "" {
			t.Errorf("allRegions should yield no badge, got %q", got)
		}
	})

	t.Run("no regions shows nothing", func(t *testing.T) {
		if got := RegionBadge(nil, false); got != "" {
			t.Errorf("empty regions should yield no badge, got %q", got)
		}
		if got := RegionBadge([]string{""}, false); got != "" {
			t.Errorf("blank region should yield no badge, got %q", got)
		}
	})

	t.Run("single region is named", func(t *testing.T) {
		got := RegionBadge([]string{"us-east-1"}, false)
		if !strings.Contains(got, "us-east-1") {
			t.Errorf("badge should name the region, got %q", got)
		}
		if !strings.Contains(got, "Region:") {
			t.Errorf("single region should use the singular label, got %q", got)
		}
	})

	t.Run("multiple regions are deduped and pluralized", func(t *testing.T) {
		got := RegionBadge([]string{"us-east-1", "eu-west-1", "us-east-1"}, false)
		if !strings.Contains(got, "us-east-1") || !strings.Contains(got, "eu-west-1") {
			t.Errorf("badge should list both regions, got %q", got)
		}
		if !strings.Contains(got, "Regions:") {
			t.Errorf("multiple regions should use the plural label, got %q", got)
		}
		if strings.Count(got, "us-east-1") != 1 {
			t.Errorf("duplicate region should appear once, got %q", got)
		}
	})
}
