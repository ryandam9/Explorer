package iamsim

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
)

func TestFromSDK(t *testing.T) {
	results := []types.EvaluationResult{{
		EvalActionName:   aws.String("s3:GetObject"),
		EvalDecision:     types.PolicyEvaluationDecisionTypeAllowed,
		EvalResourceName: aws.String("arn:aws:s3:::my-bucket/key"),
		MatchedStatements: []types.Statement{{
			SourcePolicyId:   aws.String("app-s3-read"),
			SourcePolicyType: types.PolicySourceTypeUserManaged,
		}},
		MissingContextValues: []string{"aws:SourceIp"},
		PermissionsBoundaryDecisionDetail: &types.PermissionsBoundaryDecisionDetail{
			AllowedByPermissionsBoundary: true,
		},
	}}
	vs := FromSDK(results)
	if len(vs) != 1 {
		t.Fatalf("verdicts = %d", len(vs))
	}
	v := vs[0]
	if v.Action != "s3:GetObject" || v.Decision != DecisionAllowed {
		t.Errorf("verdict = %+v", v)
	}
	if len(v.Matched) != 1 || v.Matched[0].PolicyID != "app-s3-read" || v.Matched[0].PolicyType != "user-managed" {
		t.Errorf("matched = %+v", v.Matched)
	}
	if v.BoundaryAllowed == nil || !*v.BoundaryAllowed {
		t.Errorf("boundary = %v", v.BoundaryAllowed)
	}
	if len(v.MissingContext) != 1 {
		t.Errorf("missing context = %v", v.MissingContext)
	}
}

func TestFromSDK_WildcardResourceDropped(t *testing.T) {
	vs := FromSDK([]types.EvaluationResult{{
		EvalActionName:   aws.String("ec2:DescribeInstances"),
		EvalDecision:     types.PolicyEvaluationDecisionTypeAllowed,
		EvalResourceName: aws.String("*"),
	}})
	if vs[0].Resource != "" {
		t.Errorf("Resource = %q, want empty for wildcard", vs[0].Resource)
	}
}

func render(v Verdict) string {
	var buf bytes.Buffer
	Render(&buf, "role/app", []Verdict{v})
	return buf.String()
}

func TestRender_Allowed(t *testing.T) {
	out := render(Verdict{
		Action: "s3:GetObject", Resource: "arn:aws:s3:::b/k", Decision: DecisionAllowed,
		Matched: []Statement{{PolicyID: "app-s3-read", PolicyType: "user-managed"}},
	})
	for _, want := range []string{
		"✅ Allowed: s3:GetObject on arn:aws:s3:::b/k for role/app",
		"allowed by app-s3-read (user-managed)",
		"Caveats",
		"Resource-based policies",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("allowed output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_ImplicitDeny(t *testing.T) {
	out := render(Verdict{Action: "s3:GetObject", Decision: DecisionImplicitDeny})
	for _, want := range []string{
		"❌ Denied: s3:GetObject for role/app — implicit deny",
		"no attached or inline policy allows this action",
		"Fix: grant an identity policy that allows s3:GetObject",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("implicit-deny output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_ExplicitDeny(t *testing.T) {
	out := render(Verdict{
		Action: "s3:DeleteBucket", Decision: DecisionExplicitDeny,
		Matched: []Statement{{PolicyID: "deny-destructive", PolicyType: "group"}},
	})
	for _, want := range []string{
		"EXPLICIT deny",
		"deny-destructive (group)",
		"removing an allow elsewhere will not help",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("explicit-deny output missing %q:\n%s", want, out)
		}
	}
}

func TestRender_BoundaryBlocks(t *testing.T) {
	f := false
	out := render(Verdict{Action: "s3:GetObject", Decision: DecisionImplicitDeny, BoundaryAllowed: &f})
	if !strings.Contains(out, "the boundary, not the identity policies, is the blocker") {
		t.Errorf("boundary output:\n%s", out)
	}
	// When the boundary is the blocker, we must NOT claim no identity policy
	// allows the action — the identity policy may well allow it.
	if strings.Contains(out, "no attached or inline policy allows this action") {
		t.Errorf("must not assert 'no policy allows' when boundary is the blocker:\n%s", out)
	}
	if strings.Contains(out, "(no policy allows it)") {
		t.Errorf("header must not claim 'no policy allows it' when boundary blocks:\n%s", out)
	}
}

func TestRender_MissingContextAndSCP(t *testing.T) {
	f := false
	out := render(Verdict{
		Action: "s3:GetObject", Decision: DecisionImplicitDeny,
		MissingContext: []string{"aws:SourceIp", "aws:MultiFactorAuthPresent"},
		OrgsAllowed:    &f,
	})
	if !strings.Contains(out, "aws:SourceIp, aws:MultiFactorAuthPresent") {
		t.Errorf("missing-context output:\n%s", out)
	}
	if !strings.Contains(out, "an SCP blocks this action") {
		t.Errorf("SCP output:\n%s", out)
	}
}

func TestRender_DistinctDecisions(t *testing.T) {
	a := render(Verdict{Action: "x:Y", Decision: DecisionAllowed})
	i := render(Verdict{Action: "x:Y", Decision: DecisionImplicitDeny})
	e := render(Verdict{Action: "x:Y", Decision: DecisionExplicitDeny})
	if a == i || i == e || a == e {
		t.Error("the three decisions must render distinctly")
	}
}
