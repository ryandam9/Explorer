package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
)

func roleRef() Reference {
	return Reference{Service: "iam", Type: "role", Region: "global", ID: roleARN, Name: "app"}
}

func TestAttachedPolicyEdges(t *testing.T) {
	edges := attachedPolicyEdges(roleRef(), []iamtypes.AttachedPolicy{
		{PolicyArn: aws.String("arn:aws:iam::111111111111:policy/app-policy"), PolicyName: aws.String("app-policy")},
		{PolicyName: aws.String("no-arn")}, // skipped
	})
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	if edges[0].From.Via != "attached managed policy" || edges[0].Target != "arn:aws:iam::111111111111:policy/app-policy" {
		t.Errorf("edge = %+v", edges[0])
	}
}

func TestInlinePolicyEdges(t *testing.T) {
	edges := inlinePolicyEdges(roleRef(), []string{"inline-1", ""})
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	if edges[0].Target != "app/inline-1" || edges[0].From.Via != "inline policy" {
		t.Errorf("edge = %+v", edges[0])
	}
}

func TestKMSGrantEdges_ARNsOnly(t *testing.T) {
	keyRef := Reference{Service: "kms", Type: "key", Region: "us-east-1", ID: "arn:aws:kms:us-east-1:111:key/k", Name: "k"}
	grants := []kmstypes.GrantListEntry{
		{GranteePrincipal: aws.String("arn:aws:iam::111:role/consumer")},
		{GranteePrincipal: aws.String("lambda.amazonaws.com")}, // service principal → skipped
	}
	edges := kmsGrantEdges(keyRef, grants)
	if len(edges) != 1 || edges[0].Target != "arn:aws:iam::111:role/consumer" {
		t.Fatalf("edges = %+v", edges)
	}
	if edges[0].From.Via != "grant grantee" {
		t.Errorf("via = %q", edges[0].From.Via)
	}
}

func TestKMSAliasEdges(t *testing.T) {
	edges := kmsAliasEdges([]kmstypes.AliasListEntry{
		{AliasName: aws.String("alias/app"), AliasArn: aws.String("arn:aws:kms:us-east-1:111:alias/app"), TargetKeyId: aws.String("abcd-1234")},
		{AliasName: aws.String("alias/orphan")}, // no target → skipped
	}, "us-east-1")
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.From.Type != "alias" || e.Target != "abcd-1234" || e.From.Via != "alias target key" {
		t.Errorf("edge = %+v", e)
	}
}

func TestKMSKeyPolicyEdges(t *testing.T) {
	keyRef := Reference{Service: "kms", Type: "key", Region: "us-east-1", ID: "arn:aws:kms:us-east-1:111:key/k"}
	// trustPrincipals extracts the AWS principals; kmsKeyPolicyEdges wraps them.
	principals := trustPrincipals(`{"Statement":[{"Principal":{"AWS":["arn:aws:iam::111:role/admin","*"]}}]}`)
	edges := kmsKeyPolicyEdges(keyRef, principals)
	if len(edges) != 1 || edges[0].Target != "arn:aws:iam::111:role/admin" {
		t.Fatalf("edges = %+v", edges)
	}
	if edges[0].From.Via != "key policy principal" {
		t.Errorf("via = %q", edges[0].From.Via)
	}
}

// TestDepth2_LambdaRolePolicies is the issue's acceptance criterion: a depth-2
// related query on a Lambda surfaces its role's attached policies.
func TestDepth2_LambdaRolePolicies(t *testing.T) {
	polARN := "arn:aws:iam::111111111111:policy/app-policy"
	edges := []Edge{
		{From: Reference{Service: "lambda", Type: "function", Region: "us-east-1", ID: lambdaARN, Name: "checkout", Via: "execution role"}, Target: roleARN},
	}
	edges = append(edges, attachedPolicyEdges(roleRef(), []iamtypes.AttachedPolicy{
		{PolicyArn: aws.String(polARN), PolicyName: aws.String("app-policy")},
	})...)

	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)
	res := Related(lambdaARN, fwd, rev, 2, false)

	var role, policy *Link
	for i := range res.Uses {
		switch res.Uses[i].ID {
		case roleARN:
			role = &res.Uses[i]
		case polARN:
			policy = &res.Uses[i]
		}
	}
	if role == nil || role.Depth != 1 {
		t.Fatalf("role not at depth 1: %+v", res.Uses)
	}
	if policy == nil || policy.Depth != 2 {
		t.Fatalf("policy not reached at depth 2: %+v", res.Uses)
	}
	if policy.Path != "execution role ▸ attached managed policy" {
		t.Errorf("policy path = %q", policy.Path)
	}
}

func TestCheckedTypes_SecurityRegistered(t *testing.T) {
	role := CheckedTypes(KindIAMRole)
	kms := CheckedTypes(KindKMSKey)
	if !contains(role, "KMS key policy principals") || !contains(role, "KMS key grants") {
		t.Errorf("IAM CheckedTypes missing KMS references: %v", role)
	}
	if !contains(kms, "KMS aliases") {
		t.Errorf("KMS CheckedTypes missing aliases: %v", kms)
	}
}
