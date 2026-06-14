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
	cov := Coverage(nil, typedServices, nil)
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
	cov := Coverage(resources, typedServices, nil)
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

func TestNotShown_SortedAlphabetically(t *testing.T) {
	// Everything present except eks and kms.
	resources := []model.Resource{}
	for _, c := range commonServices {
		if c.Key == "eks" || c.Key == "kms" {
			continue
		}
		resources = append(resources, model.Resource{Service: c.Key, ID: "x"})
	}
	missing := NotShown(Coverage(resources, typedServices, nil))
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing services, got %d: %+v", len(missing), missing)
	}
	// Alphabetical by label: EKS before KMS.
	if missing[0].Label != "EKS" || missing[1].Label != "KMS" {
		t.Errorf("order = %s, %s; want EKS then KMS", missing[0].Label, missing[1].Label)
	}
}

func TestCoverageNote_Empty_WhenAllShown(t *testing.T) {
	resources := make([]model.Resource, 0, len(commonServices))
	for _, c := range commonServices {
		resources = append(resources, model.Resource{Service: c.Key, ID: "x"})
	}
	if note := CoverageNote(Coverage(resources, typedServices, nil), true); note != "" {
		t.Errorf("note should be empty when every catalog service is present, got:\n%s", note)
	}
}

func TestCoverageNote_PlainLanguage(t *testing.T) {
	// Only ec2 present; the note names the missing services and explains the
	// likely cause in plain terms — no internal jargon.
	resources := []model.Resource{{Service: "ec2", ID: "i-1"}}
	note := CoverageNote(Coverage(resources, typedServices, nil), true)

	if !strings.Contains(note, "no tags") {
		t.Errorf("note should explain the tag cause in plain language:\n%s", note)
	}
	for _, jargon := range []string{"typed", "tag-discovered", "collector", "sweep"} {
		if strings.Contains(note, jargon) {
			t.Errorf("note should avoid the internal term %q:\n%s", jargon, note)
		}
	}
	// The missing services are still listed by name.
	for _, want := range []string{"Step Functions", "EKS"} {
		if !strings.Contains(note, want) {
			t.Errorf("note should list missing service %q:\n%s", want, note)
		}
	}
}

func TestCoverageNote_TypedOnlyMode(t *testing.T) {
	resources := []model.Resource{{Service: "ec2", ID: "i-1"}}
	note := CoverageNote(Coverage(resources, typedServices, nil), false)

	if !strings.Contains(note, "--typed-only") {
		t.Errorf("typed-only note should mention the flag:\n%s", note)
	}
}

func TestCatalogWith_MergesExtras(t *testing.T) {
	// A brand-new service is added; a tagged resource for it counts as shown.
	resources := []model.Resource{{Service: "apprunner", ID: "svc-1"}}
	cov := Coverage(resources, typedServices, map[string]string{"apprunner": "App Runner"})

	var found ServiceCoverage
	for _, c := range cov {
		if c.Key == "apprunner" {
			found = c
		}
	}
	if found.Label != "App Runner" {
		t.Fatalf("expected App Runner in the catalog, got %+v", found)
	}
	if !found.Shown {
		t.Error("apprunner should be shown when a resource carries that service")
	}
	// The built-in entries are still present (merge, not replace).
	if len(cov) != len(commonServices)+1 {
		t.Errorf("catalog size = %d, want defaults+1 (%d)", len(cov), len(commonServices)+1)
	}
}

func TestCatalogWith_OverridesLabelAndDefaultsBlank(t *testing.T) {
	cov := Coverage(nil, typedServices, map[string]string{
		"ec2":       "Compute (EC2)", // override a built-in label
		"apprunner": "",              // blank label falls back to the key
	})
	byKey := map[string]string{}
	for _, c := range cov {
		byKey[c.Key] = c.Label
	}
	if byKey["ec2"] != "Compute (EC2)" {
		t.Errorf("ec2 label = %q, want the override", byKey["ec2"])
	}
	if byKey["apprunner"] != "apprunner" {
		t.Errorf("blank label should fall back to the key, got %q", byKey["apprunner"])
	}
	// Overriding a label must not duplicate the entry.
	count := 0
	for _, c := range cov {
		if c.Key == "ec2" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ec2 appears %d times, want 1", count)
	}
}
