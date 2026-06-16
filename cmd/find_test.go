package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// A resource whose name would be read as a formula by a spreadsheet must be
// neutralized in find's CSV output, like every other CSV writer in the tool.
func TestRenderFindResults_CSVSanitizesFormulaInjection(t *testing.T) {
	matched := []model.Resource{
		{Service: "ec2", Type: "instance", Name: "=cmd|'/c calc'!A1", ID: "i-1", Region: "us-east-1", ARN: "arn:aws:ec2:us-east-1:1:instance/i-1"},
		{Service: "s3", Type: "bucket", Name: "+SUM(A1)", ID: "b1", Region: "global", ARN: "@evil"},
	}

	var buf bytes.Buffer
	if err := renderFindResults(&buf, matched, "csv", false); err != nil {
		t.Fatalf("renderFindResults: %v", err)
	}
	out := buf.String()

	// Dangerous leading characters must be escaped with a single quote.
	if !strings.Contains(out, "'=cmd") {
		t.Errorf("name starting with = should be prefixed with ': %q", out)
	}
	if !strings.Contains(out, "'+SUM(A1)") {
		t.Errorf("name starting with + should be prefixed with ': %q", out)
	}
	if !strings.Contains(out, "'@evil") {
		t.Errorf("ARN starting with @ should be prefixed with ': %q", out)
	}
	// A bare (unescaped) formula cell must not survive.
	if strings.Contains(out, ",=cmd") || strings.HasPrefix(out, "=cmd") {
		t.Errorf("unescaped formula cell leaked: %q", out)
	}
}
