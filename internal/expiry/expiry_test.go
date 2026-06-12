package expiry

import (
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

func kinds(items []Item) string {
	var ks []string
	for _, it := range items {
		ks = append(ks, it.Kind+":"+it.Resource)
	}
	return strings.Join(ks, ", ")
}

func TestCertItems(t *testing.T) {
	items := CertItems(now, "us-east-1", []Certificate{
		{Name: "*.example.com", NotAfter: now.Add(12 * 24 * time.Hour), InUse: true, Source: "acm"},
		{Name: "unused.example.com", NotAfter: now.Add(40 * 24 * time.Hour), Source: "acm"},
		{Name: "legacy-cert", NotAfter: now.Add(-3 * 24 * time.Hour), Source: "iam"},
		{Name: "no-date", Source: "acm"}, // zero NotAfter: skipped
	})
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (%s)", len(items), kinds(items))
	}
	if items[0].Days != 12 || !strings.Contains(items[0].Detail, "in use") {
		t.Errorf("in-use cert: %+v", items[0])
	}
	if !strings.Contains(items[1].Detail, "not attached") {
		t.Errorf("unused cert detail: %q", items[1].Detail)
	}
	if items[2].Kind != "IAM server certificate expires" || items[2].Days != -3 {
		t.Errorf("iam cert: %+v", items[2])
	}
}

func TestLambdaItems(t *testing.T) {
	items := LambdaItems(now, "us-east-1", []Function{
		{Name: "old-fn", Runtime: "python3.8"},   // deprecated 2024-10-14 (past)
		{Name: "edge-fn", Runtime: "ruby3.2"},    // deprecates 2026-03-31 (past at testNow)
		{Name: "fine-fn", Runtime: "python3.12"}, // no announced date
	})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(items), kinds(items))
	}
	if items[0].Days >= 0 || !strings.Contains(items[0].Detail, "was deprecated") {
		t.Errorf("past deprecation: %+v", items[0])
	}
	if !strings.Contains(items[0].Resource, "python3.8") {
		t.Errorf("resource should name the runtime: %q", items[0].Resource)
	}
}

func TestEKSItems(t *testing.T) {
	items := EKSItems(now, "eu-west-1", []Cluster{
		{Name: "prod", Version: "1.32"}, // ends 2026-03-23, past at testNow
		{Name: "next", Version: "1.33"}, // ends 2026-07-29, upcoming
		{Name: "edge", Version: "1.34"}, // unknown version: skipped
	})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(items), kinds(items))
	}
	if items[0].Days >= 0 {
		t.Errorf("1.32 should be past standard support: %+v", items[0])
	}
	if items[1].Days <= 0 || !strings.Contains(items[1].Detail, "extended support") {
		t.Errorf("1.33: %+v", items[1])
	}
}

func TestRDSItems(t *testing.T) {
	items := RDSItems(now, "us-east-1",
		[]DBInstance{
			{ID: "legacy-db", CACertID: "rds-ca-2019"},
			{ID: "fine-db", CACertID: "rds-ca-rsa2048-g1"},
		},
		[]Maintenance{
			{Resource: "prod-db", Action: "system-update", Description: "New OS patch",
				ForcedApply: now.Add(20 * 24 * time.Hour)},
			{Resource: "dev-db", Action: "db-upgrade",
				AutoAppliedAfter: now.Add(45 * 24 * time.Hour)},
			{Resource: "dateless-db", Action: "system-update"}, // no date: skipped
		})
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3 (%s)", len(items), kinds(items))
	}
	if items[0].Kind != "RDS CA certificate expired" || items[0].Days >= 0 {
		t.Errorf("CA item: %+v", items[0])
	}
	if !strings.Contains(items[1].Detail, "force-applied") {
		t.Errorf("forced maintenance detail: %q", items[1].Detail)
	}
	if !strings.Contains(items[2].Detail, "auto-applies") {
		t.Errorf("auto maintenance detail: %q", items[2].Detail)
	}
}

func TestSecretItems(t *testing.T) {
	items := SecretItems(now, "us-east-1", []Secret{
		// Overdue via NextRotationDate.
		{Name: "db-password", RotationEnabled: true, NextRotation: now.Add(-9 * 24 * time.Hour)},
		// Overdue via LastRotated + rule.
		{Name: "api-key", RotationEnabled: true,
			LastRotated: now.Add(-100 * 24 * time.Hour), RotateAfterDays: 30},
		// Due in the future: not a finding.
		{Name: "fresh", RotationEnabled: true, NextRotation: now.Add(5 * 24 * time.Hour)},
		// Rotation disabled: skipped.
		{Name: "manual", NextRotation: now.Add(-30 * 24 * time.Hour)},
	})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(items), kinds(items))
	}
	if items[0].Resource != "db-password" || items[0].Days != -9 {
		t.Errorf("next-rotation overdue: %+v", items[0])
	}
	if items[1].Resource != "api-key" || items[1].Days != -70 {
		t.Errorf("rule-derived overdue: %+v", items[1])
	}
}

func TestFilterAndSort(t *testing.T) {
	items := []Item{
		{Days: 120, Resource: "far"},
		{Days: 12, Resource: "soon"},
		{Days: -3, Resource: "past"},
		{Days: 90, Resource: "edge"},
		{Days: 12, Region: "a", Resource: "tie-a"},
	}
	got := Filter(items, 90)
	if len(got) != 4 {
		t.Fatalf("filtered = %d, want 4 (past always kept, 120d dropped)", len(got))
	}
	Sort(got)
	if got[0].Resource != "past" || got[3].Resource != "edge" {
		t.Errorf("order = %s", kinds(got))
	}
	// Equal days tie-break on region (empty sorts first) then resource.
	if got[1].Resource != "soon" || got[2].Resource != "tie-a" {
		t.Errorf("tie order = %s", kinds(got))
	}
}

// Every EOL table entry must parse to a real date and carry a plausible
// year — a typo'd year would silently mis-rank deadlines.
func TestEOLTablesSane(t *testing.T) {
	for runtime, date := range lambdaRuntimeDeprecation {
		if date.Year() < 2020 || date.Year() > 2035 {
			t.Errorf("lambda %s has implausible date %v", runtime, date)
		}
	}
	for version, date := range eksEndOfStandardSupport {
		if date.Year() < 2022 || date.Year() > 2035 {
			t.Errorf("eks %s has implausible date %v", version, date)
		}
		if !strings.HasPrefix(version, "1.") {
			t.Errorf("eks version key %q looks wrong", version)
		}
	}
	if _, ok := rdsCAExpiry["rds-ca-2019"]; !ok {
		t.Error("rds-ca-2019 must be in the CA table")
	}
}

func TestShortResourceID(t *testing.T) {
	if got := shortResourceID("arn:aws:rds:us-east-1:1:db:prod-db"); got != "prod-db" {
		t.Errorf("shortResourceID = %q", got)
	}
	if got := shortResourceID("plain-id"); got != "plain-id" {
		t.Errorf("plain id should pass through, got %q", got)
	}
}
