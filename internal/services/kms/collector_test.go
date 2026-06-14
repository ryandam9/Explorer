package kms

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
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
	})
	if res.Region != "eu-west-1" {
		t.Errorf("Region = %q, want eu-west-1 (from ARN)", res.Region)
	}
	if res.State != "Enabled" || res.Summary["description"] != "app data key" {
		t.Errorf("unexpected mapping: %+v", res)
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
