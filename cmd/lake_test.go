package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/traillake"
)

func resetLakeFlags() {
	lakeSQL, lakeTopPrincipals, lakeTopEvents = "", false, false
}

func TestPickStore(t *testing.T) {
	stores := []traillake.DataStore{
		{ID: "id-1", ARN: "arn:aws:cloudtrail:us-east-1:1:eventdatastore/id-1", Name: "prod"},
		{ID: "id-2", ARN: "arn:aws:cloudtrail:us-east-1:1:eventdatastore/id-2", Name: "audit"},
	}

	// No selector with several stores is an error.
	if _, err := pickStore(stores, ""); err == nil {
		t.Error("expected an error when several stores and no --store")
	}
	// The only store is used automatically.
	if s, err := pickStore(stores[:1], ""); err != nil || s.ID != "id-1" {
		t.Errorf("single store should auto-select, got %+v err=%v", s, err)
	}
	// Match by name (case-insensitive), ID, and ARN.
	for _, want := range []string{"AUDIT", "id-2", stores[1].ARN} {
		if s, err := pickStore(stores, want); err != nil || s.ID != "id-2" {
			t.Errorf("pickStore(%q) = %+v err=%v, want id-2", want, s, err)
		}
	}
	if _, err := pickStore(stores, "nope"); err == nil {
		t.Error("unknown store should error")
	}
}

func TestBuildLakeQuery(t *testing.T) {
	resetLakeFlags()
	defer resetLakeFlags()

	if _, title := buildLakeQuery("eds", traillake.Preset{}); title != "recent activity" {
		t.Errorf("default title = %q, want recent activity", title)
	}

	lakeTopPrincipals = true
	if sql, title := buildLakeQuery("eds", traillake.Preset{}); title != "top principals" || !strings.Contains(sql, "GROUP BY userIdentity.arn") {
		t.Errorf("top-principals query wrong: %q / %q", title, sql)
	}
	resetLakeFlags()

	lakeSQL = "SELECT 1 FROM eds"
	if sql, title := buildLakeQuery("eds", traillake.Preset{}); title != "custom query" || sql != "SELECT 1 FROM eds" {
		t.Errorf("raw --sql should win: %q / %q", title, sql)
	}
}

func TestParseMaxWait(t *testing.T) {
	if d, err := parseMaxWait(""); err != nil || d != 0 {
		t.Errorf("empty should be zero, got %v err=%v", d, err)
	}
	if _, err := parseMaxWait("90s"); err != nil {
		t.Errorf("90s should parse: %v", err)
	}
	if _, err := parseMaxWait("nonsense"); err == nil {
		t.Error("nonsense should error")
	}
}

func TestRenderLakeResult(t *testing.T) {
	res := traillake.Result{
		Columns: []string{"eventName", "events"},
		Rows:    [][]string{{"RunInstances", "9"}, {"DeleteBucket", "12"}},
	}

	var tbl bytes.Buffer
	if err := renderLakeResult(&tbl, res, "table", false); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"eventName", "events", "RunInstances", "12"} {
		if !strings.Contains(tbl.String(), want) {
			t.Errorf("table output missing %q:\n%s", want, tbl.String())
		}
	}

	var jsn bytes.Buffer
	if err := renderLakeResult(&jsn, res, "json", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsn.String(), `"eventName": "RunInstances"`) {
		t.Errorf("json output wrong:\n%s", jsn.String())
	}

	var csvb bytes.Buffer
	if err := renderLakeResult(&csvb, res, "csv", false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(csvb.String()), "\n")
	if lines[0] != "eventName,events" {
		t.Errorf("csv header = %q", lines[0])
	}
}
