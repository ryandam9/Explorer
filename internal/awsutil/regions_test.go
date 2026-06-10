package awsutil

import "testing"

func TestFallbackRegions(t *testing.T) {
	if len(FallbackRegions) == 0 {
		t.Fatal("FallbackRegions must not be empty")
	}

	seen := make(map[string]bool, len(FallbackRegions))
	for _, r := range FallbackRegions {
		if r == "" {
			t.Error("FallbackRegions contains an empty entry")
		}
		if seen[r] {
			t.Errorf("FallbackRegions contains duplicate %q", r)
		}
		seen[r] = true
	}

	// us-east-1 is the global/default region and must always be present so a
	// degraded "--all-regions" scan still covers it.
	if !seen["us-east-1"] {
		t.Error("FallbackRegions should include us-east-1")
	}
}
