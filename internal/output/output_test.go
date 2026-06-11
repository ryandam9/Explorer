package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestValidateFormat(t *testing.T) {
	for _, f := range []string{"table", "json", "ndjson", "csv", "TABLE", "Json"} {
		if err := ValidateFormat(f); err != nil {
			t.Errorf("ValidateFormat(%q) = %v, want nil", f, err)
		}
	}
	for _, f := range []string{"", "yaml", "tsv", "jsonl"} {
		if err := ValidateFormat(f); err == nil {
			t.Errorf("ValidateFormat(%q) = nil, want error", f)
		}
	}
}

func TestPrintErrors_Empty(t *testing.T) {
	var buf bytes.Buffer
	PrintErrors(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty error slice, got %q", buf.String())
	}
}

func TestPrintErrors_AccessDeniedErrors(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "insufficient privileges"},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "Insufficient privileges") {
		t.Errorf("expected privileges heading, got:\n%s", out)
	}
	if !strings.Contains(out, "ec2") {
		t.Errorf("expected service name ec2, got:\n%s", out)
	}
	if !strings.Contains(out, "us-east-1") {
		t.Errorf("expected region us-east-1, got:\n%s", out)
	}
}

func TestPrintErrors_OtherErrors(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "rds", Region: "eu-west-1", Code: "ThrottlingException", Message: "rate exceeded"},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if strings.Contains(out, "Insufficient privileges") {
		t.Errorf("unexpected privileges section for non-auth error:\n%s", out)
	}
	if !strings.Contains(out, "Collection errors") {
		t.Errorf("expected generic error heading:\n%s", out)
	}
	if !strings.Contains(out, "ThrottlingException") {
		t.Errorf("expected error code in output:\n%s", out)
	}
	if !strings.Contains(out, "rate exceeded") {
		t.Errorf("expected error message in output:\n%s", out)
	}
}

func TestPrintErrors_MixedErrors(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "no perms"},
		{Service: "rds", Region: "us-west-2", Code: "InternalError", Message: "server error"},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "Insufficient privileges") {
		t.Errorf("expected privileges section for AccessDenied error:\n%s", out)
	}
	if !strings.Contains(out, "Collection errors") {
		t.Errorf("expected generic error section:\n%s", out)
	}
	if !strings.Contains(out, "InternalError") {
		t.Errorf("expected non-auth error code:\n%s", out)
	}
}

// Identical errors from different regions must merge into a single entry
// listing the regions, instead of one box per region.
func TestPrintErrors_DedupesAcrossRegions(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "denied"},
		{Service: "ec2", Region: "us-west-2", Code: "AccessDenied", Message: "denied"},
		{Service: "ec2", Region: "eu-west-1", Code: "AccessDenied", Message: "denied"},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if got := strings.Count(out, "denied"); got != 1 {
		t.Errorf("expected one merged entry, message appears %d times:\n%s", got, out)
	}
	for _, region := range []string{"us-east-1", "us-west-2", "eu-west-1"} {
		if !strings.Contains(out, region) {
			t.Errorf("expected region %s in merged entry:\n%s", region, out)
		}
	}
}

func TestPrintErrors_ManyRegionsAbbreviated(t *testing.T) {
	var buf bytes.Buffer
	var errs []model.ExploreError
	for _, region := range []string{"a-1", "b-1", "c-1", "d-1", "e-1"} {
		errs = append(errs, model.ExploreError{
			Service: "ec2", Region: region, Code: "AccessDenied", Message: "denied",
		})
	}
	PrintErrors(&buf, errs)
	if !strings.Contains(buf.String(), "+2 more") {
		t.Errorf("expected abbreviated region list, got:\n%s", buf.String())
	}
}

func TestPrintErrors_PartialMarkerOnOtherErrors(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "rds", Region: "eu-west-1", Code: "ThrottlingException", Message: "rate exceeded", Partial: true},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "partial results kept") {
		t.Errorf("expected partial marker for partial error:\n%s", out)
	}
}

func TestPrintErrors_PartialMarkerOnAccessDenied(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "iam", Region: "global", Code: "AccessDenied", Message: "denied on page 2.", Partial: true},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "kept") {
		t.Errorf("expected kept-resources note for partial auth error:\n%s", out)
	}
}

func TestPrintErrors_NoPartialMarkerByDefault(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "rds", Region: "eu-west-1", Code: "InternalError", Message: "server error"},
	}
	PrintErrors(&buf, errs)
	out := buf.String()

	if strings.Contains(out, "partial") {
		t.Errorf("unexpected partial marker for non-partial error:\n%s", out)
	}
}

func TestStreamTableAlignmentStableAcrossChunks(t *testing.T) {
	chunks := make(chan model.ResultChunk, 4)
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1", Name: "a", State: "running"},
	}}
	// Second chunk arrives "later" with much longer values; earlier rows must
	// not depend on it for their column widths (fixed-width format).
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "secretsmanager", Type: "secret", Region: "ap-southeast-2", ID: "arn:aws:secretsmanager:ap-southeast-2:123456789012:secret:x", Name: "long-name", State: ""},
	}}
	close(chunks)

	var buf bytes.Buffer
	streamTable(&buf, chunks, Options{})

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected header + 2 rows, got %d lines:\n%s", len(lines), buf.String())
	}
	// The TYPE column must start at the same offset on the header and every
	// row whose SERVICE fits the floor.
	headerTypeIdx := strings.Index(lines[0], "TYPE")
	row1TypeIdx := strings.Index(lines[1], "instance")
	if headerTypeIdx == -1 || headerTypeIdx != row1TypeIdx {
		t.Errorf("TYPE column drifted: header at %d, first row at %d\n%s", headerTypeIdx, row1TypeIdx, buf.String())
	}
}

func TestStreamTableNoHeader(t *testing.T) {
	chunks := make(chan model.ResultChunk, 1)
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1", Name: "a", State: "running"},
	}}
	close(chunks)

	var buf bytes.Buffer
	streamTable(&buf, chunks, Options{NoHeader: true})
	if strings.Contains(buf.String(), "SERVICE") {
		t.Errorf("expected no header row, got:\n%s", buf.String())
	}
}

func TestStreamTableEmpty(t *testing.T) {
	chunks := make(chan model.ResultChunk)
	close(chunks)
	var buf bytes.Buffer
	streamTable(&buf, chunks, Options{})
	if !strings.Contains(buf.String(), "No resources found.") {
		t.Errorf("expected empty notice, got %q", buf.String())
	}
}

func TestStreamCSV(t *testing.T) {
	chunks := make(chan model.ResultChunk, 2)
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1", Name: "web, primary", State: "running"},
	}}
	close(chunks)

	var buf bytes.Buffer
	streamCSV(&buf, chunks, Options{})
	out := buf.String()

	if !strings.HasPrefix(out, "service,type,region,id,name,state\n") {
		t.Errorf("expected csv header, got:\n%s", out)
	}
	if !strings.Contains(out, `"web, primary"`) {
		t.Errorf("expected quoted field with comma, got:\n%s", out)
	}
}

func TestStreamNDJSON(t *testing.T) {
	chunks := make(chan model.ResultChunk, 2)
	chunks <- model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1", Name: "a", State: "running"},
		{Service: "s3", Type: "bucket", Region: "global", ID: "b", Name: "b", State: ""},
	}}
	close(chunks)

	var buf bytes.Buffer
	streamNDJSON(&buf, chunks, Options{})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 ndjson lines, got %d:\n%s", len(lines), buf.String())
	}
	for _, l := range lines {
		if !strings.HasPrefix(l, "{") || !strings.HasSuffix(l, "}") {
			t.Errorf("line is not a JSON object: %q", l)
		}
	}
}
