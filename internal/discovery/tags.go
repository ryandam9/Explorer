package discovery

import (
	"context"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	rgt "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	rgttypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// This file adds the tag-exploration calls used by the `tags` TUI on top of the
// Resource Groups Tagging API: list the tag keys in use, the values for a key,
// and the resources matching a set of tag filters. All are per-region, so each
// fans out across the requested regions (best-effort, like Discover) and merges
// the results.

// fanOutRegions runs fn for each region with bounded concurrency, merging the
// per-region slices and collecting per-region failures as ExploreErrors rather
// than aborting (§3/§6a). baseCfg supplies credentials; the region is set per call.
func fanOutRegions[T any](ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int,
	fn func(context.Context, aws.Config, string) ([]T, error)) ([]T, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	type regionResult struct {
		vals []T
		err  *model.ExploreError
	}
	results := make([]regionResult, len(regions))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			cfg := baseCfg
			cfg.Region = region
			vals, err := fn(gctx, cfg, region)
			if err != nil {
				results[i] = regionResult{err: taggingErr(region, err)}
				return nil
			}
			results[i] = regionResult{vals: vals}
			return nil
		})
	}
	_ = g.Wait()

	var all []T
	var errs []model.ExploreError
	for _, r := range results {
		all = append(all, r.vals...)
		if r.err != nil {
			errs = append(errs, *r.err)
		}
	}
	return all, errs
}

// taggingErr classifies a Resource Groups Tagging API failure for one region.
func taggingErr(region string, err error) *model.ExploreError {
	code := "CollectionError"
	msg := err.Error()
	switch {
	case awserr.IsExpiredCreds(err):
		code = "ExpiredCredentials"
		msg, _ = awserr.LoginHint(err, "")
	case awserr.IsAuthError(err):
		code = "AccessDenied"
		msg = awserr.FriendlyMessage(err, "resourcegroups")
	}
	return &model.ExploreError{Service: "resourcegroups", Region: region, Code: code, Message: msg}
}

// TagKeys returns the distinct tag keys in use across the given regions.
func TagKeys(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int) ([]string, []model.ExploreError) {
	keys, errs := fanOutRegions(ctx, baseCfg, regions, maxConcurrency,
		func(ctx context.Context, cfg aws.Config, _ string) ([]string, error) {
			client := rgt.NewFromConfig(cfg)
			var out []string
			p := rgt.NewGetTagKeysPaginator(client, &rgt.GetTagKeysInput{})
			for p.HasMorePages() {
				page, err := p.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				out = append(out, page.TagKeys...)
			}
			return out, nil
		})
	return dedupeSorted(keys), errs
}

// TagValues returns the distinct values configured for a tag key across regions.
func TagValues(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, key string) ([]string, []model.ExploreError) {
	vals, errs := fanOutRegions(ctx, baseCfg, regions, maxConcurrency,
		func(ctx context.Context, cfg aws.Config, _ string) ([]string, error) {
			client := rgt.NewFromConfig(cfg)
			var out []string
			p := rgt.NewGetTagValuesPaginator(client, &rgt.GetTagValuesInput{Key: aws.String(key)})
			for p.HasMorePages() {
				page, err := p.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				out = append(out, page.TagValues...)
			}
			return out, nil
		})
	return dedupeSorted(vals), errs
}

// discoverGroup returns the taggable resources matching one AND-group of tag
// filters (AND across keys; for a key, an empty value list means "key present",
// otherwise OR across the listed values), optionally scoped to resourceTypes
// (e.g. "ec2:instance").
func discoverGroup(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, filters map[string][]string, resourceTypes []string) ([]model.Resource, []model.ExploreError) {
	tagFilters := toTagFilters(filters)
	return fanOutRegions(ctx, baseCfg, regions, maxConcurrency,
		func(ctx context.Context, cfg aws.Config, region string) ([]model.Resource, error) {
			client := rgt.NewFromConfig(cfg)
			var out []model.Resource
			in := &rgt.GetResourcesInput{TagFilters: tagFilters}
			if len(resourceTypes) > 0 {
				in.ResourceTypeFilters = resourceTypes
			}
			p := rgt.NewGetResourcesPaginator(client, in)
			for p.HasMorePages() {
				page, err := p.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				for _, mapping := range page.ResourceTagMappingList {
					if r, ok := mapResource(region, mapping); ok {
						out = append(out, r)
					}
				}
			}
			return out, nil
		})
}

// DiscoverUnion returns the union (deduped by ARN) of the resources matching any
// of the AND-groups — i.e. groups are ORed together, since the Tagging API can
// only AND within a single GetResources call. resourceTypes scopes every group.
func DiscoverUnion(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, groups []map[string][]string, resourceTypes []string) ([]model.Resource, []model.ExploreError) {
	if len(groups) == 0 {
		groups = []map[string][]string{{}} // no tag filter → all (type filter may still apply)
	}
	var (
		all  []model.Resource
		errs []model.ExploreError
		seen = map[string]bool{}
	)
	for _, g := range groups {
		res, e := discoverGroup(ctx, baseCfg, regions, maxConcurrency, g, resourceTypes)
		errs = append(errs, e...)
		for _, r := range res {
			key := r.ARN
			if key == "" {
				key = r.Service + "|" + r.Region + "|" + r.ID
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			all = append(all, r)
		}
	}
	return all, errs
}

// CountResources counts the resources matching the tag filters without
// materializing them — used for the per-key/value counts. The count is summed
// across regions; any per-region failure is returned so the caller can mark the
// count as partial rather than wrong.
func CountResources(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, filters map[string][]string) (int, []model.ExploreError) {
	tagFilters := toTagFilters(filters)
	counts, errs := fanOutRegions(ctx, baseCfg, regions, maxConcurrency,
		func(ctx context.Context, cfg aws.Config, _ string) ([]int, error) {
			client := rgt.NewFromConfig(cfg)
			n := 0
			p := rgt.NewGetResourcesPaginator(client, &rgt.GetResourcesInput{TagFilters: tagFilters})
			for p.HasMorePages() {
				page, err := p.NextPage(ctx)
				if err != nil {
					return nil, err
				}
				n += len(page.ResourceTagMappingList)
			}
			return []int{n}, nil
		})
	total := 0
	for _, c := range counts {
		total += c
	}
	return total, errs
}

// toTagFilters builds the SDK TagFilter list from a key→values map.
func toTagFilters(filters map[string][]string) []rgttypes.TagFilter {
	out := make([]rgttypes.TagFilter, 0, len(filters))
	for k, vs := range filters {
		out = append(out, rgttypes.TagFilter{Key: aws.String(k), Values: vs})
	}
	return out
}

// dedupeSorted removes duplicates (regions can repeat keys/values) and sorts.
func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
