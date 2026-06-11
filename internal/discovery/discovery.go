// Package discovery enumerates AWS resources across every service using the
// Resource Groups Tagging API (GetResources). Unlike the typed per-service
// collectors, a single API call returns ARNs (and tags) for taggable resources
// across hundreds of services, giving the summary command broad, "all services"
// coverage without a bespoke collector per service.
//
// The trade-off: the Tagging API returns only ARNs and tags — not rich state,
// availability zones, or service-specific detail. The summary command therefore
// merges these results with the richer typed collectors, preferring the typed
// entry when both describe the same ARN.
package discovery

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	rgt "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	rgttypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Discover enumerates taggable resources across all services in each of the
// given regions. baseCfg supplies credentials; the region is overridden per
// scan. Collection errors (including access-denied) are returned alongside any
// resources found, so a failure in one region never aborts the others.
func Discover(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int) ([]model.Resource, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	type regionResult struct {
		resources []model.Resource
		err       *model.ExploreError
	}
	results := make([]regionResult, len(regions))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			res, err := discoverRegion(gctx, baseCfg, region)
			if err != nil {
				code := "CollectionError"
				msg := err.Error()
				if awserr.IsAuthError(err) {
					code = "AccessDenied"
					msg = awserr.FriendlyMessage(err, "resourcegroups")
				}
				results[i] = regionResult{err: &model.ExploreError{
					Service: "resourcegroups", Region: region, Code: code, Message: msg,
				}}
				return nil
			}
			results[i] = regionResult{resources: res}
			return nil
		})
	}
	_ = g.Wait()

	var resources []model.Resource
	var errs []model.ExploreError
	for _, r := range results {
		resources = append(resources, r.resources...)
		if r.err != nil {
			errs = append(errs, *r.err)
		}
	}
	return resources, errs
}

// discoverRegion pages GetResources for a single region.
func discoverRegion(ctx context.Context, baseCfg aws.Config, region string) ([]model.Resource, error) {
	cfg := baseCfg
	cfg.Region = region
	client := rgt.NewFromConfig(cfg)

	var resources []model.Resource
	paginator := rgt.NewGetResourcesPaginator(client, &rgt.GetResourcesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get resources in %s: %w", region, err)
		}
		for _, mapping := range page.ResourceTagMappingList {
			if r, ok := mapResource(region, mapping); ok {
				resources = append(resources, r)
			}
		}
	}
	return resources, nil
}

// mapResource converts a Tagging API result into a normalized Resource by
// parsing its ARN. Returns false when the ARN cannot be parsed.
func mapResource(scanRegion string, mapping rgttypes.ResourceTagMapping) (model.Resource, bool) {
	arnStr := aws.ToString(mapping.ResourceARN)
	parsed, ok := awsutil.ParseARN(arnStr)
	if !ok {
		return model.Resource{}, false
	}

	tags := make(map[string]string, len(mapping.Tags))
	for _, t := range mapping.Tags {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}

	// Prefer the ARN's own region; global resources (e.g. IAM, S3) carry an
	// empty region in their ARN, so fall back to the region being scanned.
	region := parsed.Region
	if region == "" {
		region = "global"
	}

	name := tags["Name"]
	if name == "" {
		name = parsed.ARNName()
	}

	return model.Resource{
		Service:   parsed.Service,
		Type:      parsed.ResourceType,
		Region:    region,
		AccountID: parsed.AccountID,
		ID:        parsed.ResourceID,
		Name:      name,
		ARN:       arnStr,
		Tags:      tags,
	}, true
}
