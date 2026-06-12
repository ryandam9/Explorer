package authzmsg

import (
	"strings"
	"testing"
)

// implicitDenyDoc is the shape sts:DecodeAuthorizationMessage returns for a
// request no policy allows.
const implicitDenyDoc = `{
  "allowed": false,
  "explicitDeny": false,
  "matchedStatements": {"items": []},
  "failures": {"items": []},
  "context": {
    "principal": {"id": "AIDAEXAMPLE", "arn": "arn:aws:iam::123456789012:user/bob"},
    "action": "ec2:RunInstances",
    "resource": "arn:aws:ec2:us-east-1:123456789012:instance/*",
    "conditions": {"items": []}
  }
}`

const explicitDenyDoc = `{
  "allowed": false,
  "explicitDeny": true,
  "matchedStatements": {"items": [
    {"statementId": "DenyProd", "effect": "DENY", "sourcePolicyId": "guardrails", "sourcePolicyType": "IAM Policy"}
  ]},
  "context": {
    "principal": {"id": "AIDAEXAMPLE"},
    "action": "s3:DeleteBucket",
    "resource": "arn:aws:s3:::prod-data"
  }
}`

func TestSummarizeImplicitDeny(t *testing.T) {
	s, err := Summarize([]byte(implicitDenyDoc))
	if err != nil {
		t.Fatal(err)
	}
	if s.Allowed || s.ExplicitDeny {
		t.Errorf("allowed/explicitDeny = %v/%v, want false/false", s.Allowed, s.ExplicitDeny)
	}
	if s.Principal != "arn:aws:iam::123456789012:user/bob" {
		t.Errorf("Principal = %q (ARN should win over ID)", s.Principal)
	}
	if s.Action != "ec2:RunInstances" || !strings.Contains(s.Resource, "instance/*") {
		t.Errorf("Action/Resource = %q / %q", s.Action, s.Resource)
	}
	if len(s.MatchedStatements) != 0 {
		t.Errorf("MatchedStatements = %v, want none", s.MatchedStatements)
	}
}

func TestSummarizeExplicitDeny(t *testing.T) {
	s, err := Summarize([]byte(explicitDenyDoc))
	if err != nil {
		t.Fatal(err)
	}
	if !s.ExplicitDeny {
		t.Error("ExplicitDeny should be true")
	}
	if s.Principal != "AIDAEXAMPLE" {
		t.Errorf("Principal = %q (should fall back to ID without an ARN)", s.Principal)
	}
	if len(s.MatchedStatements) != 1 || s.MatchedStatements[0] != "guardrails (IAM Policy)" {
		t.Errorf("MatchedStatements = %v", s.MatchedStatements)
	}
}

func TestSummarizeInvalidJSON(t *testing.T) {
	if _, err := Summarize([]byte("not json")); err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestRenderVerdicts(t *testing.T) {
	implicit, _ := Summarize([]byte(implicitDenyDoc))
	out := Render(implicit)
	for _, want := range []string{"Implicit deny", "user/bob", "ec2:RunInstances", "Fix:"} {
		if !strings.Contains(out, want) {
			t.Errorf("implicit-deny render missing %q:\n%s", want, out)
		}
	}

	explicit, _ := Summarize([]byte(explicitDenyDoc))
	out = Render(explicit)
	for _, want := range []string{"Explicit deny", "guardrails (IAM Policy)", "scope it down"} {
		if !strings.Contains(out, want) {
			t.Errorf("explicit-deny render missing %q:\n%s", want, out)
		}
	}

	out = Render(Summary{Allowed: true, Action: "s3:GetObject"})
	if !strings.Contains(out, "✓ Allowed") || strings.Contains(out, "Fix:") {
		t.Errorf("allowed render = %q", out)
	}
}

func TestStripPrefix(t *testing.T) {
	cases := map[string]string{
		"AQoDYXdzEJr...blob": "AQoDYXdzEJr...blob",
		"  AQoDYXdz  ":       "AQoDYXdz",
		`"AQoDYXdz"`:         "AQoDYXdz",
		"AQoDYXdz trailing":  "AQoDYXdz",
		"An error occurred (UnauthorizedOperation): You are not authorized. Encoded authorization failure message: AQoDYXdzEJr12345": "AQoDYXdzEJr12345",
		"Encoded authorization failure message:\n  AQoDYXdz":                                                                         "AQoDYXdz",
		"":    "",
		"   ": "",
	}
	for in, want := range cases {
		if got := StripPrefix(in); got != want {
			t.Errorf("StripPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
