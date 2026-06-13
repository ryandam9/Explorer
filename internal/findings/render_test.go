package findings

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func sampleFindings() []Finding {
	return []Finding{
		{
			ID: "COST-EBS-001", Severity: SevWarning, Service: "ec2", Region: "us-east-1",
			Resource: "vol-0abc", Title: "Unattached EBS volume (gp2, 1024 GiB)",
			Detail: "still bills", Fix: "Snapshot then delete, or attach",
			EstMonthlyUSD: 102.40,
		},
		{
			ID: "COST-EIP-001", Severity: SevWarning, Service: "ec2", Region: "us-east-1",
			Resource: "eipalloc-1", Title: "Elastic IP not associated",
			Detail: "bills hourly", Fix: "Release the address",
			EstMonthlyUSD: 3.65,
		},
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleFindings(), "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"SNO", "SEVERITY", "EST/MO", // header
		"COST-EBS-001", "vol-0abc", "$102.40",
		"🟡 WARNING",
		"$106.05/month", // total
		"0 critical, 2 warning, 0 info",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTableNoHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleFindings(), "table", true); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "SEVERITY") {
		t.Error("noHeader output still contains header")
	}
}

func TestRenderTableZeroEstimateHasNoSavingsLine(t *testing.T) {
	var buf bytes.Buffer
	fs := []Finding{{ID: "X-001", Severity: SevInfo, Resource: "r", Title: "t"}}
	if err := Render(&buf, fs, "table", false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "savings") {
		t.Errorf("zero-estimate output should not claim savings:\n%s", buf.String())
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleFindings(), "json", false); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Findings        []Finding `json:"findings"`
		TotalMonthlyUSD float64   `json:"totalMonthlyUSD"`
	}
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(report.Findings) != 2 {
		t.Errorf("findings = %d, want 2", len(report.Findings))
	}
	if diff := report.TotalMonthlyUSD - 106.05; diff > 0.001 || diff < -0.001 {
		t.Errorf("total = %v, want ≈106.05", report.TotalMonthlyUSD)
	}
}

func TestRenderJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, nil, "json", false); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, `"findings": []`) {
		t.Errorf("empty findings should marshal as [], got:\n%s", out)
	}
}

func TestRenderNDJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleFindings(), "ndjson", false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("ndjson lines = %d, want 2", len(lines))
	}
	var f Finding
	if err := json.Unmarshal([]byte(lines[0]), &f); err != nil {
		t.Fatalf("line 0 not valid JSON: %v", err)
	}
}

func TestRenderCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleFindings(), "csv", false); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 { // header + 2
		t.Fatalf("csv records = %d, want 3", len(recs))
	}
	if recs[1][0] != "WARNING" || recs[1][1] != "COST-EBS-001" || recs[1][6] != "102.40" {
		t.Errorf("csv row = %v", recs[1])
	}
}
