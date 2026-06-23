package xref

import (
	"strings"
	"testing"
)

func TestAssessRisk(t *testing.T) {
	mk := func(n int, partial bool) RelatedResult {
		var ub []Link
		for i := 0; i < n; i++ {
			ub = append(ub, Link{Reference: Reference{ID: "x"}, Depth: 1})
		}
		// A deeper-hop dependent must not count toward direct blast radius.
		ub = append(ub, Link{Reference: Reference{ID: "deep"}, Depth: 2})
		return RelatedResult{UsedBy: ub, Partial: partial}
	}

	cases := []struct {
		direct int
		want   RiskLevel
	}{
		{0, RiskLow},
		{1, RiskMedium},
		{2, RiskMedium},
		{3, RiskHigh},
		{9, RiskHigh},
	}
	for _, c := range cases {
		got := AssessRisk(mk(c.direct, false))
		if got.Level != c.want {
			t.Errorf("AssessRisk(direct=%d).Level = %s, want %s (reason %q)", c.direct, got.Level, c.want, got.Reason)
		}
	}

	// Partial scans are flagged as possibly understating risk.
	if got := AssessRisk(mk(0, true)); !strings.Contains(got.Reason, "partial") {
		t.Errorf("partial scan should be noted in the reason: %q", got.Reason)
	}
}
