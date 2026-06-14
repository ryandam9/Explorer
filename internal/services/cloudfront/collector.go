// Package cloudfront collects CloudFront distributions. CloudFront is a global
// service, so the collector runs once (not per region) and lists every
// distribution in the account.
//
// A typed collector is needed because the Resource Groups Tagging API that
// powers broad discovery only returns resources that are tagged (or were
// previously tagged); a distribution that has never been tagged is invisible to
// it. ListDistributions returns them all, tagged or not.
package cloudfront

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "cloudfront"
}

func (c *Collector) IsGlobal() bool {
	return true
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	// CloudFront has a single global endpoint signed in us-east-1; pin the
	// region so the call works regardless of the caller's default region.
	cfg := input.AWSConfig
	cfg.Region = "us-east-1"
	client := cloudfront.NewFromConfig(cfg)

	var resources []model.Resource

	var marker *string
	for {
		output, err := client.ListDistributions(ctx, &cloudfront.ListDistributionsInput{
			Marker: marker,
		})
		if err != nil {
			return resources, fmt.Errorf("failed to list CloudFront distributions: %w", err)
		}

		list := output.DistributionList
		if list == nil {
			break
		}

		batch := make([]model.Resource, 0, len(list.Items))
		for _, dist := range list.Items {
			batch = append(batch, c.mapDistribution(dist))
		}
		resources = input.EmitOrAppend(resources, batch)

		if !aws.ToBool(list.IsTruncated) {
			break
		}
		marker = list.NextMarker
	}

	return resources, nil
}

func (c *Collector) mapDistribution(dist types.DistributionSummary) model.Resource {
	id := aws.ToString(dist.Id)
	domain := aws.ToString(dist.DomainName)

	// State reflects whether the distribution serves traffic: the toggle
	// (enabled/disabled) takes precedence, with the deployment status when it
	// is enabled, so the summary distinguishes a live distribution from one
	// that is enabled but still propagating.
	state := aws.ToString(dist.Status)
	if !aws.ToBool(dist.Enabled) {
		state = "Disabled"
	}

	// The distribution's human-facing name is its first alternate domain name
	// (CNAME) when one is set, falling back to the assigned *.cloudfront.net
	// domain — distributions have no Name attribute of their own.
	name := domain
	if dist.Aliases != nil && len(dist.Aliases.Items) > 0 {
		name = dist.Aliases.Items[0]
	}

	res := model.Resource{
		Service: "cloudfront",
		Type:    "distribution",
		Region:  "global",
		ID:      id,
		Name:    name,
		ARN:     aws.ToString(dist.ARN),
		State:   state,
		Summary: map[string]string{
			"domainName": domain,
			"enabled":    fmt.Sprintf("%t", aws.ToBool(dist.Enabled)),
			"status":     aws.ToString(dist.Status),
		},
	}

	if t := dist.LastModifiedTime; t != nil {
		res.Summary["lastModified"] = t.UTC().Format("2006-01-02 15:04:05 UTC")
	}
	if comment := aws.ToString(dist.Comment); comment != "" {
		res.Summary["comment"] = comment
	}
	if dist.Aliases != nil && len(dist.Aliases.Items) > 0 {
		res.Summary["aliases"] = strings.Join(dist.Aliases.Items, ", ")
	}

	return res
}
