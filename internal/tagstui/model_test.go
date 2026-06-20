package tagstui

import (
	"reflect"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestParseFilterExpr(t *testing.T) {
	cases := map[string]map[string][]string{
		"Env=prod":                    {"Env": {"prod"}},
		"Env=prod, Team=payments":     {"Env": {"prod"}, "Team": {"payments"}},
		"Team=payments, Team=billing": {"Team": {"payments", "billing"}}, // repeated key → OR
		"Owner":                       {"Owner": nil},                    // bare key → key present
		" Env = prod ":                {"Env": {"prod"}},                 // trimmed
		"":                            {},
		",, ,":                        {},
		"=value":                      {},           // empty key dropped
		"Env=":                        {"Env": nil}, // key present, no value
	}
	for in, want := range cases {
		if got := parseFilterExpr(in); !reflect.DeepEqual(got, want) {
			t.Errorf("parseFilterExpr(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestFilterDesc(t *testing.T) {
	// Deterministic, sorted, with OR values joined by "|".
	got := filterDesc(map[string][]string{"Team": {"billing", "payments"}, "Env": {"prod"}, "Owner": nil})
	want := "Env=prod, Owner, Team=billing|payments"
	if got != want {
		t.Errorf("filterDesc = %q, want %q", got, want)
	}
}

func TestResourceRows(t *testing.T) {
	rows := resourceRows([]model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", Region: "us-east-1", ID: "i-123"},
		{Service: "s3", Type: "bucket", Region: "us-east-1", ID: "my-bucket"}, // no Name → "—"
	})
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0][0] != "1" || rows[0][1] != "ec2" || rows[0][3] != "web" {
		t.Errorf("row 0 = %v", rows[0])
	}
	if rows[1][3] != "—" {
		t.Errorf("missing name should render as em dash, got %q", rows[1][3])
	}
}

func TestCountCell(t *testing.T) {
	counts := map[string]countVal{
		"done":    {n: 5, complete: true},
		"partial": {n: 3, complete: false},
	}
	cases := map[string]string{"done": "5", "partial": "3+", "missing": "…"}
	for item, want := range cases {
		if got := countCell(counts, item); got != want {
			t.Errorf("countCell(%q) = %q, want %q", item, got, want)
		}
	}
}

func TestKeyRowsIncludeCount(t *testing.T) {
	rows := keyRows([]string{"Env", "Team"}, map[string]countVal{"Env": {n: 9, complete: true}})
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	// #, key, count — Env resolved to 9, Team still counting (…).
	if rows[0][1] != "Env" || rows[0][2] != "9" {
		t.Errorf("row 0 = %v", rows[0])
	}
	if rows[1][2] != "…" {
		t.Errorf("uncounted row should show …, got %q", rows[1][2])
	}
}
