// Package tagstui is the interactive "explore resources by tag" dashboard. It
// drills tag keys → values → resources, or jumps straight to resources from a
// typed "Key=Value" filter, over the Resource Groups Tagging API (reusing
// internal/discovery for the per-region fan-out, pagination and ARN mapping).
//
// Coverage caveat (surfaced in the UI): the Tagging API returns only *tagged*
// resources, and only services that integrate with it (IAM, for example, is not
// covered) — so this is "resources tagged & known to the Resource Groups Tagging
// API", never every resource.
package tagstui

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/discovery"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const maxConcurrency = 8

// Client resolves the regions to query once and serves the three tag lookups.
type Client struct {
	base    aws.Config
	regions []string
}

// NewClient builds the base AWS config and resolves the region scope (a single
// region, or every enabled region when allRegions is set).
func NewClient(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool) (*Client, error) {
	bootstrap := "us-east-1"
	if len(regions) > 0 {
		bootstrap = regions[0]
	}
	base, err := auth.BuildAWSConfig(ctx, awsCfg, bootstrap)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}
	resolved := awsutil.ResolveRegions(ctx, base, regions, allRegions)
	if len(resolved) == 0 {
		resolved = []string{bootstrap}
	}
	return &Client{base: base, regions: resolved}, nil
}

// Regions returns the regions this client queries.
func (c *Client) Regions() []string { return c.regions }

// TagKeys lists the distinct tag keys in use across the scoped regions.
func (c *Client) TagKeys(ctx context.Context) ([]string, []model.ExploreError) {
	return discovery.TagKeys(ctx, c.base, c.regions, maxConcurrency)
}

// TagValues lists the distinct values configured for a tag key.
func (c *Client) TagValues(ctx context.Context, key string) ([]string, []model.ExploreError) {
	return discovery.TagValues(ctx, c.base, c.regions, maxConcurrency, key)
}

// Resources lists the resources matching the given tag filters (AND across
// keys; OR across a key's values; empty values = "key present").
func (c *Client) Resources(ctx context.Context, filters map[string][]string) ([]model.Resource, []model.ExploreError) {
	return discovery.DiscoverWithFilters(ctx, c.base, c.regions, maxConcurrency, filters)
}
