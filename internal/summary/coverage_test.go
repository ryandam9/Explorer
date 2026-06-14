package summary

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

var typedServices = []string{
	"ec2", "s3", "rds", "iam", "dynamodb", "lambda", "emr", "ecs", "eks",
	"elbv2", "secretsmanager", "sqs", "sns", "cloudwatch", "cloudfront", "route53",
}

func coverageFor(t *testing.T, key string) ServiceCoverage {
	t.Helper()
	cov := Coverage(nil, typedServices)
	for _, c := range cov {
		if c.Key == key {
			return c
		}
	}
	t.Fatalf("service %q not in catalog", key)
	return ServiceCoverage{}
}

func TestCoverage_TypedFlagFromRegistry(t *testing.T) {
	// A registered collector name is typed; a tag-discovered namespace is not.
	if c := coverageFor(t, "cloudfront"); !c.Typed {
		t.Error("cloudfront should be typed (it has a collector)")
	}
	if c := coverageFor(t, "elasticache"); c.Typed {
		t.Error("elasticache should be tag-discovered, not typed (no collector in this set)")
	}
}

func TestCoverage_ShownReflectsResources(t *testing.T) {
	resources := []model.Resource{
		{Service: "ec2", ID: "i-1"},
		{Service: "elasticache", ID: "cc-1"}, // a tagged ElastiCache cluster
	}
	cov := Coverage(resources, typedServices)
	byKey := map[string]ServiceCoverage{}
	for _, c := range cov {
		byKey[c.Key] = c
	}
	if !byKey["ec2"].Shown {
		t.Error("ec2 should be shown")
	}
	if !byKey["elasticache"].Shown {
		t.Error("elasticache should be shown when a resource carries that service")
	}
	if byKey["lambda"].Shown {
		t.Error("lambda should not be shown when no lambda resources exist")
	}
}

func TestNotShown_OrdersTagDiscoveredFirst(t *testing.T) {
	// Everything present except one typed (eks) and one tag-only (kms) service.
	resources := []model.Resource{}
	for _, c := range commonServices {
		if c.Key == "eks" || c.Key == "kms" {
			continue
		}
		resources = append(resources, model.Resource{Service: c.Key, ID: "x"})
	}
	missing := NotShown(Coverage(resources, typedServices))
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing services, got %d: %+v", len(missing), missing)
	}
	// Tag-discovered (kms) sorts before typed (eks).
	if missing[0].Key != "kms" || missing[1].Key != "eks" {
		t.Errorf("order = %s, %s; want kms then eks", missing[0].Key, missing[1].Key)
	}
}

func TestCoverageNote_Empty_WhenAllShown(t *testing.T) {
	resources := make([]model.Resource, 0, len(commonServices))
	for _, c := range commonServices {
		resources = append(resources, model.Resource{Service: c.Key, ID: "x"})
	}
	if note := CoverageNote(Coverage(resources, typedServices), len(typedServices), true); note != "" {
		t.Errorf("note should be empty when every catalog service is present, got:\n%s", note)
	}
}

func TestCoverageNote_TagSweepCaveat(t *testing.T) {
	// Only ec2 present; a tag-only service (Step Functions) and a typed one
	// (EKS) are both missing, and each gets the right reason.
	resources := []model.Resource{{Service: "ec2", ID: "i-1"}}
	note := CoverageNote(Coverage(resources, typedServices), len(typedServices), true)

	if !strings.Contains(note, "Coverage") {
		t.Errorf("note missing heading:\n%s", note)
	}
	if !strings.Contains(note, "Step Functions — tag-discovered: none found, or present but untagged (hidden)") {
		t.Errorf("note missing tag-discovered reason for Step Functions:\n%s", note)
	}
	if !strings.Contains(note, "EKS — typed: none found") {
		t.Errorf("note missing typed reason for EKS:\n%s", note)
	}
}

func TestCoverageNote_TypedOnlyMode(t *testing.T) {
	// With the sweep skipped, tag-discovered services are "not collected".
	resources := []model.Resource{{Service: "ec2", ID: "i-1"}}
	note := CoverageNote(Coverage(resources, typedServices), len(typedServices), false)

	if !strings.Contains(note, "--typed-only") {
		t.Errorf("typed-only note should mention the flag:\n%s", note)
	}
	if !strings.Contains(note, "Step Functions — tag-discovered: not collected (sweep skipped)") {
		t.Errorf("typed-only note should say tag-discovered services were not collected:\n%s", note)
	}
}
