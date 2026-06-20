package tagstui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestRenderStringsJSON(t *testing.T) {
	var b bytes.Buffer
	if err := RenderStrings(&b, []string{"Env", "Team"}, "Tag key", "json", false); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, b.String())
	}
	if len(got) != 2 || got[0] != "Env" {
		t.Errorf("got %v", got)
	}

	// nil → "[]" (valid empty array), not "null".
	b.Reset()
	_ = RenderStrings(&b, nil, "Tag key", "json", false)
	if strings.TrimSpace(b.String()) != "[]" {
		t.Errorf("nil should render []; got %q", b.String())
	}
}

func TestRenderStringsCSVInjection(t *testing.T) {
	var b bytes.Buffer
	if err := RenderStrings(&b, []string{"=cmd()", "safe"}, "Tag key", "csv", false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "Tag key") {
		t.Errorf("missing header: %q", out)
	}
	// A formula-injection cell must be neutralized (prefixed), not emitted raw.
	if strings.Contains(out, "\n=cmd()") || strings.HasPrefix(out, "=cmd()") {
		t.Errorf("CSV formula injection not neutralized:\n%s", out)
	}
}

func TestRenderResourcesCSV(t *testing.T) {
	var b bytes.Buffer
	res := []model.Resource{{
		Service: "ec2", Type: "instance", Name: "web", Region: "us-east-1",
		ID: "i-1", ARN: "arn:aws:ec2:us-east-1:1:instance/i-1",
		Tags: map[string]string{"Team": "pay", "Env": "prod"},
	}}
	if err := RenderResources(&b, res, "csv", false); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "Service,Type,Name,Region,AccountID,ID,ARN,Tags") {
		t.Errorf("missing header row:\n%s", out)
	}
	// Tags are stable/sorted "k=v;k2=v2".
	if !strings.Contains(out, "Env=prod;Team=pay") {
		t.Errorf("tags column = unexpected:\n%s", out)
	}
}

func TestRenderResourcesJSONEmpty(t *testing.T) {
	var b bytes.Buffer
	_ = RenderResources(&b, nil, "json", false)
	if strings.TrimSpace(b.String()) != "[]" {
		t.Errorf("nil resources should render []; got %q", b.String())
	}
}
