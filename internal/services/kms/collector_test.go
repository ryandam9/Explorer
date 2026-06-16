package kms

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "kms" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapKey_RegionFromARN(t *testing.T) {
	res := NewCollector().mapKey(&types.KeyMetadata{
		KeyId:       aws.String("abcd-1234"),
		Arn:         aws.String("arn:aws:kms:eu-west-1:1:key/abcd-1234"),
		KeyState:    types.KeyStateEnabled,
		Description: aws.String("app data key"),
	}, nil)
	if res.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1 (from ARN)", res.Region)
	}
	if res.State != "Enabled" || res.Summary["description"] != "app data key" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	// Without an alias the display name falls back to the key ID.
	if res.Name != "abcd-1234" {
		t.Errorf("Name = %q, want the key ID when no alias", res.Name)
	}
}

func TestMapKey_AliasBecomesName(t *testing.T) {
	res := NewCollector().mapKey(&types.KeyMetadata{
		KeyId:    aws.String("k-1"),
		Arn:      aws.String("arn:aws:kms:us-east-1:1:key/k-1"),
		KeyState: types.KeyStateEnabled,
	}, []string{"alias/app", "alias/app-legacy"})
	if res.Name != "alias/app" {
		t.Errorf("Name = %q, want first alias", res.Name)
	}
	if res.Summary["aliases"] != "alias/app, alias/app-legacy" {
		t.Errorf("aliases summary = %q", res.Summary["aliases"])
	}
}

func TestRegionFromARN(t *testing.T) {
	if got := regionFromARN("arn:aws:kms:ap-south-1:1:key/x"); got != "ap-south-1" {
		t.Errorf("regionFromARN = %q", got)
	}
	if got := regionFromARN("malformed"); got != "" {
		t.Errorf("malformed ARN should yield empty region, got %q", got)
	}
}

// fakeKMS implements kmsAPI with single-page ListKeys/ListAliases and a
// per-keyID DescribeKey lookup (missing entry => error).
type fakeKMS struct {
	keys     []types.KeyListEntry
	aliases  []types.AliasListEntry
	managers map[string]types.KeyManagerType // keyID -> manager (absent => DescribeKey errors)
}

func (f fakeKMS) ListKeys(context.Context, *kms.ListKeysInput, ...func(*kms.Options)) (*kms.ListKeysOutput, error) {
	return &kms.ListKeysOutput{Keys: f.keys}, nil
}

func (f fakeKMS) ListAliases(context.Context, *kms.ListAliasesInput, ...func(*kms.Options)) (*kms.ListAliasesOutput, error) {
	return &kms.ListAliasesOutput{Aliases: f.aliases}, nil
}

func (f fakeKMS) DescribeKey(_ context.Context, in *kms.DescribeKeyInput, _ ...func(*kms.Options)) (*kms.DescribeKeyOutput, error) {
	id := aws.ToString(in.KeyId)
	mgr, ok := f.managers[id]
	if !ok {
		return nil, errors.New("AccessDenied: DescribeKey " + id)
	}
	return &kms.DescribeKeyOutput{KeyMetadata: &types.KeyMetadata{
		KeyId:       in.KeyId,
		Arn:         aws.String("arn:aws:kms:us-east-1:1:key/" + id),
		KeyState:    types.KeyStateEnabled,
		KeyManager:  mgr,
		Description: aws.String("desc-" + id),
	}}, nil
}

func key(id string) types.KeyListEntry { return types.KeyListEntry{KeyId: aws.String(id)} }

func TestCollect_JoinsErrorsExcludesAWSManagedMapsAliases(t *testing.T) {
	api := fakeKMS{
		keys: []types.KeyListEntry{key("cust1"), key("cust2"), key("awskey"), key("err1"), key("err2")},
		aliases: []types.AliasListEntry{
			{AliasName: aws.String("alias/app"), TargetKeyId: aws.String("cust1")},
			{AliasName: aws.String("alias/orphan")}, // no target — ignored
		},
		managers: map[string]types.KeyManagerType{
			"cust1":  types.KeyManagerTypeCustomer,
			"cust2":  types.KeyManagerTypeCustomer,
			"awskey": types.KeyManagerTypeAws,
			// err1, err2 absent => DescribeKey errors
		},
	}

	resources, err := (&Collector{}).collect(context.Background(), api, services.CollectInput{})

	// Both describe failures must be reported, not just the first.
	if err == nil || !strings.Contains(err.Error(), "err1") || !strings.Contains(err.Error(), "err2") {
		t.Fatalf("expected a joined error naming err1 and err2, got: %v", err)
	}

	byID := map[string]model.Resource{}
	for _, r := range resources {
		byID[r.ID] = r
	}
	if _, ok := byID["awskey"]; ok {
		t.Error("AWS-managed key should be excluded")
	}
	if len(byID) != 2 {
		t.Fatalf("expected 2 customer keys, got %d: %v", len(byID), resources)
	}
	if byID["cust1"].Name != "alias/app" {
		t.Errorf("cust1 should be named by its alias, got %q", byID["cust1"].Name)
	}
	if byID["cust2"].Name != "cust2" {
		t.Errorf("cust2 has no alias; name should be the key ID, got %q", byID["cust2"].Name)
	}
}
