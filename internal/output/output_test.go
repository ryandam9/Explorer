package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/user/aws_explorer/internal/model"
)

func TestPrintErrors_Empty(t *testing.T) {
	var buf bytes.Buffer
	printErrors(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for empty error slice, got %q", buf.String())
	}
}

func TestPrintErrors_AccessDeniedErrors(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "insufficient privileges"},
	}
	printErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "INSUFFICIENT PRIVILEGES") {
		t.Errorf("expected privilege box header, got:\n%s", out)
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
	printErrors(&buf, errs)
	out := buf.String()

	if strings.Contains(out, "INSUFFICIENT PRIVILEGES") {
		t.Errorf("unexpected privilege box for non-auth error:\n%s", out)
	}
	if !strings.Contains(out, "Errors encountered during collection") {
		t.Errorf("expected generic error header:\n%s", out)
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
	printErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "INSUFFICIENT PRIVILEGES") {
		t.Errorf("expected privilege box for AccessDenied error:\n%s", out)
	}
	if !strings.Contains(out, "Errors encountered during collection") {
		t.Errorf("expected generic error section:\n%s", out)
	}
	if !strings.Contains(out, "InternalError") {
		t.Errorf("expected non-auth error code:\n%s", out)
	}
}

func TestPrintErrors_MultipleAccessDenied(t *testing.T) {
	var buf bytes.Buffer
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "denied for ec2"},
		{Service: "s3", Region: "global", Code: "AccessDenied", Message: "denied for s3"},
	}
	printErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "ec2") {
		t.Errorf("expected ec2 in output:\n%s", out)
	}
	if !strings.Contains(out, "s3") {
		t.Errorf("expected s3 in output:\n%s", out)
	}
}

func TestPrintErrors_LongMessageWraps(t *testing.T) {
	var buf bytes.Buffer
	longMsg := "Insufficient privileges — required IAM permission: ec2:DescribeInstances to list all instance resources in the region"
	errs := []model.ExploreError{
		{Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: longMsg},
	}
	printErrors(&buf, errs)
	out := buf.String()

	if !strings.Contains(out, "INSUFFICIENT PRIVILEGES") {
		t.Errorf("expected privilege box:\n%s", out)
	}
}
