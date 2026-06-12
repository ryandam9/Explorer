package findings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSortOrdersSeverityThenCost(t *testing.T) {
	fs := []Finding{
		{ID: "c", Severity: SevInfo, Resource: "r3", EstMonthlyUSD: 100},
		{ID: "a", Severity: SevWarning, Resource: "r2", EstMonthlyUSD: 5},
		{ID: "b", Severity: SevWarning, Resource: "r1", EstMonthlyUSD: 50},
		{ID: "d", Severity: SevCritical, Resource: "r4"},
	}
	Sort(fs)
	gotIDs := []string{fs[0].ID, fs[1].ID, fs[2].ID, fs[3].ID}
	want := []string{"d", "b", "a", "c"}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Fatalf("order = %v, want %v", gotIDs, want)
		}
	}
}

func TestSortStableTieBreaks(t *testing.T) {
	fs := []Finding{
		{ID: "x", Severity: SevWarning, Region: "us-west-2", Resource: "b"},
		{ID: "y", Severity: SevWarning, Region: "us-east-1", Resource: "a"},
	}
	Sort(fs)
	if fs[0].ID != "y" {
		t.Errorf("ties should order by region then resource; got %v first", fs[0].ID)
	}
}

func TestTotalMonthlyUSD(t *testing.T) {
	fs := []Finding{{EstMonthlyUSD: 10.5}, {EstMonthlyUSD: 2.25}, {}}
	if got := TotalMonthlyUSD(fs); got != 12.75 {
		t.Errorf("Total = %v, want 12.75", got)
	}
}

func TestCountBySeverityAndSummary(t *testing.T) {
	fs := []Finding{
		{Severity: SevCritical},
		{Severity: SevWarning}, {Severity: SevWarning},
		{Severity: SevInfo},
	}
	crit, warn, info := CountBySeverity(fs)
	if crit != 1 || warn != 2 || info != 1 {
		t.Errorf("counts = %d/%d/%d, want 1/2/1", crit, warn, info)
	}
	if got := Summary(fs); got != "1 critical, 2 warning, 1 info" {
		t.Errorf("Summary = %q", got)
	}
}

func TestSeverityJSONMarshalsAsString(t *testing.T) {
	b, err := json.Marshal(Finding{ID: "COST-EBS-001", Severity: SevWarning})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"severity":"WARNING"`) {
		t.Errorf("severity not marshaled as string: %s", b)
	}
}
