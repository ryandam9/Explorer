package gluetui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestGlueAboutTextNoLiteralEscapes guards against an escaping slip: paragraph
// breaks must be real newlines, not a literal backslash-n the overlay would
// render verbatim.
func TestGlueAboutTextNoLiteralEscapes(t *testing.T) {
	if strings.Contains(glueAboutText, `\n`) {
		t.Errorf("glueAboutText contains a literal \\n escape:\n%s", glueAboutText)
	}
}

func TestFilterRunsByStatus(t *testing.T) {
	runs := []JobRun{
		{ID: "1", State: "SUCCEEDED"},
		{ID: "2", State: "FAILED"},
		{ID: "3", State: "failed"},
	}
	if got := FilterRunsByStatus(runs, ""); len(got) != 3 {
		t.Errorf("empty status should return all, got %d", len(got))
	}
	got := FilterRunsByStatus(runs, "failed")
	if len(got) != 2 {
		t.Fatalf("FAILED filter = %d runs, want 2", len(got))
	}
}

func TestRenderRunsJSON(t *testing.T) {
	runs := []JobRun{{
		ID: "jr_1", State: "SUCCEEDED",
		Started:  time.Date(2026, 6, 15, 1, 14, 0, 0, time.UTC),
		ExecSecs: 742, DPUSeconds: 7416, Worker: "G.1X ×10", Attempt: 1,
	}}
	var buf bytes.Buffer
	if err := RenderRuns(&buf, runs, "json", false); err != nil {
		t.Fatal(err)
	}
	var decoded []runJSON
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded) != 1 {
		t.Fatalf("got %d runs", len(decoded))
	}
	r := decoded[0]
	if r.DurationSeconds != 742 || r.Attempt != 1 || r.State != "SUCCEEDED" {
		t.Errorf("unexpected run: %+v", r)
	}
	if r.DPUHours < 2.05 || r.DPUHours > 2.07 {
		t.Errorf("dpuHours = %v, want ≈2.06", r.DPUHours)
	}
	if r.EstUSD < 0.90 || r.EstUSD > 0.91 {
		t.Errorf("estUsd = %v, want ≈0.906", r.EstUSD)
	}
	if r.Started != "2026-06-15T01:14:00Z" {
		t.Errorf("started = %q", r.Started)
	}
}

func TestRenderJobsTable(t *testing.T) {
	jobs := []Job{{
		Name: "etl", Region: "us-east-1", LastRunState: "FAILED",
		LastRunSeconds: 161, Worker: "G.1X ×10", GlueVersion: "4.0",
	}}
	var buf bytes.Buffer
	if err := RenderJobs(&buf, jobs, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"NAME", "etl", "us-east-1", "FAILED", "2m 41s", "G.1X ×10", "4.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}
}

func TestRenderCrawlersCSV(t *testing.T) {
	crawlers := []Crawler{{Name: "c1", Region: "us-east-1", State: "READY", LastCrawlStatus: "SUCCEEDED", Database: "sales"}}
	var buf bytes.Buffer
	if err := RenderCrawlers(&buf, crawlers, "csv", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Name,Region,State") || !strings.Contains(out, "c1,us-east-1,READY,SUCCEEDED,sales") {
		t.Errorf("unexpected CSV:\n%s", out)
	}
}
