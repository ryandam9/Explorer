package cmd

import (
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// TestApplyGlobalAWSOverrides_RegionWins verifies that --region overrides every
// other region setting: config aws.regions, aws.allRegions, --all-regions, and
// filters.regions.
func TestApplyGlobalAWSOverrides_RegionWins(t *testing.T) {
	prevAll, prevRegion := allRegions, summaryRegion
	t.Cleanup(func() { allRegions, summaryRegion = prevAll, prevRegion })

	AppConfig = &config.Config{}
	AppConfig.AWS.Regions = []string{"eu-west-1", "us-east-1"}
	AppConfig.AWS.AllRegions = true
	AppConfig.Filters.Regions = []string{"eu-west-1"}

	allRegions = true // --all-regions also set; --region must still win
	summaryRegion = "ap-southeast-2"

	applyGlobalAWSOverrides()

	if got := AppConfig.AWS.Regions; len(got) != 1 || got[0] != "ap-southeast-2" {
		t.Fatalf("AWS.Regions = %v, want [ap-southeast-2]", got)
	}
	if AppConfig.AWS.AllRegions {
		t.Error("--region should disable AllRegions")
	}
	if len(AppConfig.Filters.Regions) != 0 {
		t.Errorf("--region should clear Filters.Regions, got %v", AppConfig.Filters.Regions)
	}
}

// TestApplyGlobalAWSOverrides_NoRegionLeavesConfig confirms the config's own
// region settings are untouched when --region is not passed.
func TestApplyGlobalAWSOverrides_NoRegionLeavesConfig(t *testing.T) {
	prevAll, prevRegion := allRegions, summaryRegion
	t.Cleanup(func() { allRegions, summaryRegion = prevAll, prevRegion })

	AppConfig = &config.Config{}
	AppConfig.AWS.Regions = []string{"eu-west-1"}

	allRegions = false
	summaryRegion = ""

	applyGlobalAWSOverrides()

	if got := AppConfig.AWS.Regions; len(got) != 1 || got[0] != "eu-west-1" {
		t.Fatalf("AWS.Regions = %v, want [eu-west-1] unchanged", got)
	}
}
