package cloudfront

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "cloudfront" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cloudfront")
	}
	if !c.IsGlobal() {
		t.Error("IsGlobal() = false, want true — CloudFront is a global service")
	}
}

func TestMapDistribution_BasicFields(t *testing.T) {
	c := NewCollector()
	modified := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	dist := types.DistributionSummary{
		Id:               aws.String("EDFDVBD632BHDS5"),
		ARN:              aws.String("arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5"),
		DomainName:       aws.String("d111111abcdef8.cloudfront.net"),
		Enabled:          aws.Bool(true),
		Status:           aws.String("Deployed"),
		Comment:          aws.String("prod CDN"),
		LastModifiedTime: aws.Time(modified),
		Aliases:          &types.Aliases{Items: []string{"cdn.example.com", "assets.example.com"}},
	}

	res := c.mapDistribution(dist)

	if res.Service != "cloudfront" || res.Type != "distribution" {
		t.Errorf("Service/Type = %q/%q, want cloudfront/distribution", res.Service, res.Type)
	}
	if res.Region != "global" {
		t.Errorf("Region = %q, want global", res.Region)
	}
	if res.ID != "EDFDVBD632BHDS5" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.ARN != "arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5" {
		t.Errorf("ARN = %q", res.ARN)
	}
	// An enabled, deployed distribution carries its deployment status as state.
	if res.State != "Deployed" {
		t.Errorf("State = %q, want Deployed", res.State)
	}
	// Name prefers the first alternate domain name (CNAME) over the assigned
	// cloudfront.net domain.
	if res.Name != "cdn.example.com" {
		t.Errorf("Name = %q, want cdn.example.com", res.Name)
	}
	if res.Summary["domainName"] != "d111111abcdef8.cloudfront.net" {
		t.Errorf("Summary[domainName] = %q", res.Summary["domainName"])
	}
	if res.Summary["enabled"] != "true" {
		t.Errorf("Summary[enabled] = %q, want true", res.Summary["enabled"])
	}
	if res.Summary["aliases"] != "cdn.example.com, assets.example.com" {
		t.Errorf("Summary[aliases] = %q", res.Summary["aliases"])
	}
	if res.Summary["comment"] != "prod CDN" {
		t.Errorf("Summary[comment] = %q", res.Summary["comment"])
	}
	if res.Summary["lastModified"] != "2026-06-01 12:00:00 UTC" {
		t.Errorf("Summary[lastModified] = %q", res.Summary["lastModified"])
	}
}

func TestMapDistribution_Disabled(t *testing.T) {
	c := NewCollector()
	// A disabled distribution reports "Disabled" as its state even though its
	// deployment status is still "Deployed".
	dist := types.DistributionSummary{
		Id:         aws.String("E2DISABLED"),
		ARN:        aws.String("arn:aws:cloudfront::123456789012:distribution/E2DISABLED"),
		DomainName: aws.String("d2.cloudfront.net"),
		Enabled:    aws.Bool(false),
		Status:     aws.String("Deployed"),
	}

	res := c.mapDistribution(dist)

	if res.State != "Disabled" {
		t.Errorf("State = %q, want Disabled", res.State)
	}
	if res.Summary["enabled"] != "false" {
		t.Errorf("Summary[enabled] = %q, want false", res.Summary["enabled"])
	}
}

func TestMapDistribution_NoAliasesFallsBackToDomain(t *testing.T) {
	c := NewCollector()
	// With no alternate domain names, the assigned cloudfront.net domain is the
	// distribution's name, and no aliases summary key is set.
	dist := types.DistributionSummary{
		Id:         aws.String("E3NOALIAS"),
		ARN:        aws.String("arn:aws:cloudfront::123456789012:distribution/E3NOALIAS"),
		DomainName: aws.String("d3.cloudfront.net"),
		Enabled:    aws.Bool(true),
		Status:     aws.String("InProgress"),
		Aliases:    &types.Aliases{Items: nil},
	}

	res := c.mapDistribution(dist)

	if res.Name != "d3.cloudfront.net" {
		t.Errorf("Name = %q, want d3.cloudfront.net", res.Name)
	}
	if _, ok := res.Summary["aliases"]; ok {
		t.Error("Summary[aliases] should be absent when there are no alternate domain names")
	}
	if res.State != "InProgress" {
		t.Errorf("State = %q, want InProgress", res.State)
	}
}
