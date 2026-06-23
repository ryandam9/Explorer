package tui

import (
	"slices"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// pair reports whether args contains flag immediately followed by value.
func pair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestRelatedJumpArgs(t *testing.T) {
	regional := model.Resource{ARN: "arn:aws:iam::1:role/app", Region: "us-east-1"}

	t.Run("all-regions session preserves scope, never pins region", func(t *testing.T) {
		got := relatedJumpArgs(&config.Config{AWS: config.AWSConfig{AllRegions: true}}, "", regional)
		if !slices.Contains(got, "--all-regions") {
			t.Errorf("expected --all-regions in %v", got)
		}
		if slices.Contains(got, "--region") {
			t.Errorf("must not pin --region in all-regions mode: %v", got)
		}
	})

	t.Run("single-region session pins the resource region", func(t *testing.T) {
		got := relatedJumpArgs(&config.Config{}, "", regional)
		if !pair(got, "--region", "us-east-1") {
			t.Errorf("expected --region us-east-1 in %v", got)
		}
		if slices.Contains(got, "--all-regions") {
			t.Errorf("must not pass --all-regions for a single-region session: %v", got)
		}
	})

	t.Run("global resource gets no region flag", func(t *testing.T) {
		got := relatedJumpArgs(&config.Config{}, "", model.Resource{ARN: "arn:aws:iam::1:role/g", Region: "global"})
		if slices.Contains(got, "--region") || slices.Contains(got, "--all-regions") {
			t.Errorf("global resource should carry no region flag: %v", got)
		}
	})

	t.Run("profile is preserved", func(t *testing.T) {
		got := relatedJumpArgs(&config.Config{AWS: config.AWSConfig{Profile: "prod"}}, "", regional)
		if !pair(got, "--profile", "prod") {
			t.Errorf("expected --profile prod in %v", got)
		}
	})

	t.Run("nil config does not panic and pins region", func(t *testing.T) {
		got := relatedJumpArgs(nil, "", regional)
		if !pair(got, "--region", "us-east-1") {
			t.Errorf("nil cfg should still pin the resource region: %v", got)
		}
	})
}
