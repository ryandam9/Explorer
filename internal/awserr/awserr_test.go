package awserr

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
)

// mockAPIError implements smithy.APIError for testing.
type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) Error() string              { return e.message }
func (e *mockAPIError) ErrorCode() string          { return e.code }
func (e *mockAPIError) ErrorMessage() string       { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func TestIsAuthError_NilError(t *testing.T) {
	if IsAuthError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsAuthError_NonAPIError(t *testing.T) {
	if IsAuthError(errors.New("generic error")) {
		t.Error("expected false for plain error")
	}
}

func TestIsAuthError_KnownAuthCodes(t *testing.T) {
	codes := []string{
		"AccessDenied",
		"AccessDeniedException",
		"UnauthorizedOperation",
		"AuthorizationError",
		"Forbidden",
	}
	for _, code := range codes {
		err := &mockAPIError{code: code, message: "permission denied"}
		if !IsAuthError(err) {
			t.Errorf("expected true for error code %q", code)
		}
	}
}

func TestIsAuthError_WrappedAuthError(t *testing.T) {
	inner := &mockAPIError{code: "AccessDenied", message: "denied"}
	wrapped := fmt.Errorf("describe failed: %w", inner)
	if !IsAuthError(wrapped) {
		t.Error("expected true for wrapped AccessDenied error")
	}
}

func TestIsAuthError_UnknownCode(t *testing.T) {
	err := &mockAPIError{code: "InvalidParameter", message: "bad param"}
	if IsAuthError(err) {
		t.Error("expected false for non-auth error code")
	}
}

func TestFriendlyMessage_NonAPIError(t *testing.T) {
	err := errors.New("connection refused")
	got := FriendlyMessage(err, "ec2")
	if got != "connection refused" {
		t.Errorf("got %q, want %q", got, "connection refused")
	}
}

func TestFriendlyMessage_ExtractsActionFromMessage(t *testing.T) {
	msg := "User arn:aws:iam::123456789012:user/alice is not authorized to perform: ec2:DescribeInstances on resource: *"
	err := &mockAPIError{code: "AccessDenied", message: msg}
	got := FriendlyMessage(err, "ec2")
	want := "Insufficient privileges — required IAM permission: ec2:DescribeInstances"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyMessage_ExtractsActionWithTabSeparator(t *testing.T) {
	msg := "User x is not authorized to perform: s3:ListBuckets\ton resource"
	err := &mockAPIError{code: "AccessDenied", message: msg}
	got := FriendlyMessage(err, "s3")
	if !strings.Contains(got, "s3:ListBuckets") {
		t.Errorf("expected extracted action in message, got %q", got)
	}
}

func TestFriendlyMessage_FallsBackToServiceHint_EC2(t *testing.T) {
	err := &mockAPIError{code: "AccessDenied", message: "Access denied (no action marker)"}
	got := FriendlyMessage(err, "ec2")
	if !strings.Contains(got, "ec2:DescribeInstances") {
		t.Errorf("expected ec2 service hint, got %q", got)
	}
	if !strings.Contains(got, "EC2") {
		t.Errorf("expected uppercase service name, got %q", got)
	}
}

func TestFriendlyMessage_FallsBackToServiceHint_IAM(t *testing.T) {
	err := &mockAPIError{code: "AccessDenied", message: "Denied"}
	got := FriendlyMessage(err, "iam")
	if !strings.Contains(got, "iam:ListUsers") {
		t.Errorf("expected iam service hint, got %q", got)
	}
}

func TestFriendlyMessage_UnknownService(t *testing.T) {
	err := &mockAPIError{code: "Forbidden", message: "Forbidden"}
	got := FriendlyMessage(err, "unknownservice")
	want := "Insufficient privileges (Forbidden)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFriendlyMessage_AllKnownServicesHaveHints(t *testing.T) {
	services := []string{
		"ec2", "s3", "rds", "iam", "dynamodb", "lambda",
		"emr", "ecs", "eks", "elbv2", "secretsmanager",
		"sqs", "sns", "cloudwatch", "route53",
	}
	for _, svc := range services {
		err := &mockAPIError{code: "AccessDenied", message: "no marker here"}
		got := FriendlyMessage(err, svc)
		if strings.Contains(got, "Insufficient privileges (") {
			t.Errorf("service %q fell through to generic message: %q", svc, got)
		}
	}
}
