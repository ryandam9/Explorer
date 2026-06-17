package lambdatui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleFunctions() []Function {
	return []Function{
		{Name: "orders", Region: "us-east-1", ARN: "arn:fn:orders", Runtime: "python3.12", PackageType: "Zip", MemoryMB: 256, TimeoutSec: 30, State: "Active", LastModified: time.Date(2026, 6, 15, 1, 14, 0, 0, time.UTC), DLQTargetArn: "arn:aws:sqs:us-east-1:1:dl"},
		{Name: "img", Region: "us-east-1", ARN: "arn:fn:img", PackageType: "Image", MemoryMB: 1024, TimeoutSec: 900, State: "Active"},
	}
}

func TestRenderFunctionsJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderFunctions(&buf, sampleFunctions(), "json", false); err != nil {
		t.Fatal(err)
	}
	var got []functionJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 2 || got[0].Name != "orders" || !got[0].HasDLQ {
		t.Errorf("unexpected JSON: %+v", got)
	}
	if got[1].HasDLQ {
		t.Error("img has no DLQ")
	}
}

func TestRenderFunctionsCSVHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderFunctions(&buf, sampleFunctions(), "csv", false); err != nil {
		t.Fatal(err)
	}
	first := strings.SplitN(buf.String(), "\n", 2)[0]
	if !strings.HasPrefix(first, "Name,Region,Runtime") {
		t.Errorf("CSV header = %q", first)
	}
	// noHeader omits it.
	buf.Reset()
	_ = RenderFunctions(&buf, sampleFunctions(), "csv", true)
	if strings.HasPrefix(buf.String(), "Name,Region") {
		t.Error("noHeader should omit the CSV header row")
	}
}

func TestRenderFunctionsTable(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderFunctions(&buf, sampleFunctions(), "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "orders") || !strings.Contains(out, "Image") {
		t.Errorf("table output missing expected content:\n%s", out)
	}
}

func TestRenderLayersJSON(t *testing.T) {
	var buf bytes.Buffer
	layers := []Layer{{Name: "deps", Region: "us-east-1", ARN: "arn:layer:deps", LatestVersion: 7, Runtimes: []string{"python3.12"}}}
	if err := RenderLayers(&buf, layers, "json", false); err != nil {
		t.Fatal(err)
	}
	var got []layerJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].LatestVersion != 7 {
		t.Errorf("unexpected layers JSON: %+v", got)
	}
}

func TestRenderEventSourcesJSON(t *testing.T) {
	var buf bytes.Buffer
	es := []EventSource{{UUID: "u1", Region: "us-east-1", FunctionName: "orders", SourceLabel: "sqs:q", State: "Enabled", BatchSize: 10}}
	if err := RenderEventSources(&buf, es, "json", false); err != nil {
		t.Fatal(err)
	}
	var got []eventSourceJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].Function != "orders" || got[0].BatchSize != 10 {
		t.Errorf("unexpected event-source JSON: %+v", got)
	}
}
