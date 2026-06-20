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
	// Compact columns: #, Name, Type, Region, ID (#333 three-column layout).
	if rows[0][0] != "1" || rows[0][1] != "web" || rows[0][2] != "instance" || rows[0][4] != "i-123" {
		t.Errorf("row 0 = %v", rows[0])
	}
	if rows[1][1] != "—" {
		t.Errorf("missing name should render as em dash, got %q", rows[1][1])
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

func TestParseQueryOR(t *testing.T) {
	// Two AND-groups ORed with "||".
	groups, types := ParseQuery("Env=prod, Team=pay || Env=staging")
	if len(groups) != 2 || len(types) != 0 {
		t.Fatalf("groups=%v types=%v", groups, types)
	}
	if !reflect.DeepEqual(groups[0], map[string][]string{"Env": {"prod"}, "Team": {"pay"}}) {
		t.Errorf("group0 = %#v", groups[0])
	}
	if !reflect.DeepEqual(groups[1], map[string][]string{"Env": {"staging"}}) {
		t.Errorf("group1 = %#v", groups[1])
	}
}

func TestParseQueryType(t *testing.T) {
	// type: terms are pulled out as resource-type scopes (deduped), not tag filters.
	groups, types := ParseQuery("Env=prod, type:ec2:instance || type:ec2:instance, type:s3:bucket")
	if !reflect.DeepEqual(types, []string{"ec2:instance", "s3:bucket"}) {
		t.Errorf("types = %#v", types)
	}
	// Only the Env=prod group remains (the second group had only type terms).
	if len(groups) != 1 || !reflect.DeepEqual(groups[0], map[string][]string{"Env": {"prod"}}) {
		t.Errorf("groups = %#v", groups)
	}
}

func TestParseQueryEmpty(t *testing.T) {
	if g, ty := ParseQuery("  ||  , "); len(g) != 0 || len(ty) != 0 {
		t.Errorf("empty query → g=%v ty=%v", g, ty)
	}
}

func TestQueryDesc(t *testing.T) {
	got := queryDesc([]map[string][]string{{"Team": {"pay"}}, {"Env": {"prod"}}}, []string{"s3:bucket", "ec2:instance"})
	if got != "Team=pay || Env=prod · type:ec2:instance|s3:bucket" {
		t.Errorf("queryDesc = %q", got)
	}
}
