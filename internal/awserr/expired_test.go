package awserr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsExpiredCreds_Codes(t *testing.T) {
	for _, code := range []string{"ExpiredToken", "ExpiredTokenException", "RequestExpired", "InvalidGrantException"} {
		err := &mockAPIError{code: code, message: "whatever"}
		if !IsExpiredCreds(err) {
			t.Errorf("code %s should classify as expired", code)
		}
		// Wrapped, as the SDK delivers them.
		if !IsExpiredCreds(fmt.Errorf("operation failed: %w", err)) {
			t.Errorf("wrapped code %s should classify as expired", code)
		}
	}
}

func TestIsExpiredCreds_Phrases(t *testing.T) {
	msgs := []string{
		"failed to refresh cached credentials, failed to read cached SSO token file",
		"The security token included in the request is expired",
		"SSO session has expired or is invalid",
		"the SSO Token has expired and refresh failed",
	}
	for _, m := range msgs {
		if !IsExpiredCreds(errors.New(m)) {
			t.Errorf("message should classify as expired: %q", m)
		}
	}
}

func TestIsExpiredCreds_Negative(t *testing.T) {
	cases := []error{
		nil,
		errors.New("connection reset by peer"),
		&mockAPIError{code: "AccessDenied", message: "not authorized to perform: ec2:DescribeInstances"},
		&mockAPIError{code: "Throttling", message: "rate exceeded"},
		// The SDK says "failed to refresh cached credentials" for ANY provider
		// failure — here there are no credentials at all, which a login hint
		// would misdescribe.
		errors.New("operation error STS: DecodeAuthorizationMessage, get identity: get credentials: " +
			"failed to refresh cached credentials, no EC2 IMDS role found, operation error ec2imds: GetMetadata, " +
			"http response error StatusCode: 403, request to EC2 IMDS failed"),
	}
	for _, err := range cases {
		if IsExpiredCreds(err) {
			t.Errorf("should not classify as expired: %v", err)
		}
	}
}

func TestLoginHint_SSOWithProfile(t *testing.T) {
	err := errors.New("failed to refresh cached credentials, the SSO session has expired")
	hint, ok := LoginHint(err, "prod")
	if !ok {
		t.Fatal("expected a hint")
	}
	if !strings.Contains(hint, "aws sso login --profile prod") {
		t.Errorf("hint should name the exact command: %q", hint)
	}
	if !strings.Contains(hint, "profile 'prod'") {
		t.Errorf("hint should name the profile: %q", hint)
	}
}

func TestLoginHint_SSODefaultProfile(t *testing.T) {
	err := errors.New("SSO session has expired")
	for _, profile := range []string{"", "default"} {
		hint, ok := LoginHint(err, profile)
		if !ok {
			t.Fatal("expected a hint")
		}
		if strings.Contains(hint, "--profile") {
			t.Errorf("default profile should not add --profile: %q", hint)
		}
	}
}

func TestLoginHint_NonSSOExpired(t *testing.T) {
	err := &mockAPIError{code: "ExpiredToken", message: "The security token included in the request is expired"}
	hint, ok := LoginHint(err, "staging")
	if !ok {
		t.Fatal("expected a hint")
	}
	if !strings.Contains(hint, "expired") || !strings.Contains(hint, "staging") {
		t.Errorf("hint = %q", hint)
	}
}

func TestLoginHint_NotExpired(t *testing.T) {
	if _, ok := LoginHint(errors.New("access denied"), "p"); ok {
		t.Error("non-expired errors should yield no hint")
	}
	if _, ok := LoginHint(nil, "p"); ok {
		t.Error("nil error should yield no hint")
	}
}
