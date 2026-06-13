package quotas

import (
	"strings"
	"testing"
)

func TestEvaluate_StatusThresholds(t *testing.T) {
	in := []Quota{
		{Name: "VPCs per Region", Service: "vpc", Region: "us-east-1", Limit: 5, Used: 5, UsageKnown: true},     // 100% critical
		{Name: "Standard vCPUs", Service: "ec2", Region: "us-east-1", Limit: 64, Used: 58, UsageKnown: true},    // 90.6% warn
		{Name: "Elastic IPs", Service: "ec2", Region: "us-east-1", Limit: 5, Used: 1, UsageKnown: true},         // 20% ok
		{Name: "Roles per account", Service: "iam", Region: "global", Limit: 1000, UsageKnown: false},           // unknown
	}
	rows := Evaluate(in, 80)

	byName := map[string]Row{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	if byName["VPCs per Region"].Status != StatusCritical {
		t.Errorf("VPCs status = %q, want critical", byName["VPCs per Region"].Status)
	}
	if byName["Standard vCPUs"].Status != StatusWarn {
		t.Errorf("vCPUs status = %q, want warn", byName["Standard vCPUs"].Status)
	}
	if byName["Elastic IPs"].Status != StatusOK {
		t.Errorf("EIP status = %q, want ok", byName["Elastic IPs"].Status)
	}
	if byName["Roles per account"].Status != StatusUnknown {
		t.Errorf("Roles status = %q, want unknown", byName["Roles per account"].Status)
	}
	if byName["Roles per account"].Percent != nil {
		t.Errorf("unknown-usage row must have nil Percent")
	}
	// Sorted most-severe first: critical leads, unknown last.
	if rows[0].Name != "VPCs per Region" {
		t.Errorf("first row = %q, want VPCs per Region (critical)", rows[0].Name)
	}
	if rows[len(rows)-1].Name != "Roles per account" {
		t.Errorf("last row = %q, want Roles per account (unknown)", rows[len(rows)-1].Name)
	}
}

func TestEvaluate_PercentComputation(t *testing.T) {
	rows := Evaluate([]Quota{{Name: "q", Limit: 64, Used: 58, UsageKnown: true}}, 80)
	if rows[0].Percent == nil {
		t.Fatal("Percent is nil")
	}
	if got := *rows[0].Percent; got < 90.6 || got > 90.7 {
		t.Errorf("Percent = %.2f, want ~90.6", got)
	}
	if rows[0].Used == nil || *rows[0].Used != 58 {
		t.Errorf("Used = %v, want 58", rows[0].Used)
	}
}

func TestEvaluate_ZeroLimitNoPanic(t *testing.T) {
	rows := Evaluate([]Quota{{Name: "z", Limit: 0, Used: 0, UsageKnown: true}}, 80)
	if rows[0].Percent == nil || *rows[0].Percent != 0 {
		t.Errorf("zero-limit Percent = %v, want 0", rows[0].Percent)
	}
	if rows[0].Status != StatusOK {
		t.Errorf("zero-limit status = %q, want ok", rows[0].Status)
	}
}

func TestFilter(t *testing.T) {
	rows := Evaluate([]Quota{
		{Name: "high", Limit: 10, Used: 9, UsageKnown: true},  // 90%
		{Name: "low", Limit: 10, Used: 1, UsageKnown: true},   // 10%
		{Name: "unk", Limit: 10, UsageKnown: false},           // unknown
	}, 80)

	kept, dropped := Filter(rows, 80)
	if len(kept) != 1 || kept[0].Name != "high" {
		t.Errorf("Filter(80) kept = %v, want [high]", names(kept))
	}
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}

	all, d0 := Filter(rows, 0)
	if len(all) != 3 || d0 != 0 {
		t.Errorf("Filter(0) = %d rows, %d dropped; want 3, 0", len(all), d0)
	}
}

func TestRender_TableAndJSON(t *testing.T) {
	rows := Evaluate([]Quota{
		{Name: "VPCs per Region", Service: "vpc", Region: "us-east-1", Limit: 5, Used: 5, UsageKnown: true},
		{Name: "Standard vCPUs", Service: "ec2", Region: "us-east-1", Limit: 64, Used: 58, UsageKnown: true, Unit: ""},
	}, 80)

	var sb strings.Builder
	if err := Render(&sb, rows, "table", false); err != nil {
		t.Fatalf("table: %v", err)
	}
	out := sb.String()
	for _, want := range []string{"VPCs per Region", "5 / 5", "100%", "CRITICAL", "91%", "WARN"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	sb.Reset()
	if err := Render(&sb, rows, "json", false); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(sb.String(), `"status": "critical"`) {
		t.Errorf("json missing critical status:\n%s", sb.String())
	}
}

func names(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
