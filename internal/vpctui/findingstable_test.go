package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBuildFindingsTable(t *testing.T) {
	findings := []Finding{
		{
			Severity: SevCritical,
			Resource: "sg-0123456789abcdef0",
			Title:    "Security group exposes a sensitive port to the internet",
			Detail:   "sg-0123456789abcdef0: Allow inbound SSH (TCP 22) from anywhere on the internet (0.0.0.0/0)",
			Fix:      "Restrict the source to specific CIDRs or a security group instead of 0.0.0.0/0.",
		},
		{
			Severity: SevWarning,
			Resource: "rtb-1",
			Title:    "Route table has a blackhole route",
			Detail:   "rtb-1: route to 10.9.0.0/16 points at a deleted target.",
			Fix:      "Remove or repoint the stale route.",
		},
	}
	out := ansi.Strip(buildFindingsTable(findings, 120))

	for _, want := range []string{
		"SEVERITY", "RESOURCE", "ISSUE", "FIX",
		"CRITICAL", "sg-0123456789abcdef0", "sensitive port",
		"WARNING", "rtb-1", "blackhole",
		"Restrict the source", "Remove or repoint",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("findings table missing %q:\n%s", want, out)
		}
	}

	// Header comes first, and the severity column starts each data row.
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "SEVERITY") {
		t.Errorf("expected header row first, got %q", lines[0])
	}

	// Long cells wrap within the table width instead of overflowing.
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w > 120 {
			t.Errorf("line %d exceeds table width (%d > 120): %q", i, w, ln)
		}
	}
}

func TestBuildFindingsTableNarrowWidth(t *testing.T) {
	findings := []Finding{{
		Severity: SevInfo,
		Resource: "subnet-1",
		Title:    "Subnet has no outbound internet path",
		Fix:      "Add a NAT gateway route if internet access is needed.",
	}}
	// Must not panic or produce negative widths on tiny terminals.
	out := ansi.Strip(buildFindingsTable(findings, 20))
	if !strings.Contains(out, "subnet-1") {
		t.Errorf("narrow table should still render the resource, got:\n%s", out)
	}
}
