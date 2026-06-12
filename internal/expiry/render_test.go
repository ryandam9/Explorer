package expiry

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleItems() []Item {
	d := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	return []Item{
		{Date: d, Days: -3, Kind: "Lambda runtime deprecated", Resource: "payments-fn (python3.8)",
			Region: "us-east-1", Detail: "update the runtime"},
		{Date: d.Add(15 * 24 * time.Hour), Days: 12, Kind: "ACM certificate expires",
			Resource: "*.example.com", Region: "us-east-1", Detail: "renew it"},
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleItems(), "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"DAYS", "WHAT", "-3", "payments-fn", "12", "*.example.com"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
	// Sorted input renders in order: the expired item line comes first.
	if strings.Index(out, "payments-fn") > strings.Index(out, "example.com") {
		t.Error("expired item should render before upcoming one")
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleItems(), "json", false); err != nil {
		t.Fatal(err)
	}
	var items []Item
	if err := json.Unmarshal(buf.Bytes(), &items); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(items) != 2 || items[0].Days != -3 {
		t.Errorf("items = %+v", items)
	}

	buf.Reset()
	if err := Render(&buf, nil, "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(buf.String()), "[]") {
		t.Errorf("empty items should marshal as [], got %q", buf.String())
	}
}

func TestRenderCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleItems(), "csv", false); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("csv records = %d, want 3", len(recs))
	}
	if recs[1][0] != "-3" || recs[1][1] != "2026-06-09" {
		t.Errorf("csv row = %v", recs[1])
	}
}

func TestRenderNDJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleItems(), "ndjson", false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("ndjson lines = %d, want 2", len(lines))
	}
}
