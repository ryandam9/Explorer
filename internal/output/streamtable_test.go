package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// TestStreamTableSequenceNumbers verifies the default scan table carries an SNO
// column whose numbering is 1-based and continuous across streamed chunks.
func TestStreamTableSequenceNumbers(t *testing.T) {
	chunks := make(chan model.ResultChunk, 2)
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1"},
		{Service: "s3", Type: "bucket", Region: "global", ID: "b-2"},
	}}
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "rds", Type: "db", Region: "us-east-1", ID: "d-3"},
	}}
	close(chunks)

	var buf bytes.Buffer
	streamTable(&buf, chunks, Options{})
	out := buf.String()

	if !strings.Contains(out, "SNO") {
		t.Fatalf("scan table missing SNO header:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// Header + three resource rows.
	if len(lines) < 4 {
		t.Fatalf("expected a header and 3 rows, got %d lines:\n%s", len(lines), out)
	}
	for i, want := range []string{"1", "2", "3"} {
		row := lines[i+1] // skip the header line
		if !strings.HasPrefix(strings.TrimSpace(row), want) {
			t.Errorf("row %d should start with %q (continuous across chunks); got %q", i+1, want, row)
		}
	}
}
