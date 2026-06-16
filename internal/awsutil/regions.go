package awsutil

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/ryandam9/aws_explorer/internal/awserr"
)

// ResolveRegions returns the region set a command should scan, so every
// command (engine, trail, audit, TUIs) resolves regions identically.
//
//   - all == true, or the keyword "all" present in requested: every region
//     from ec2:DescribeRegions, falling back to FallbackRegions when that call
//     is denied or fails (best-effort, never aborts the run).
//   - otherwise: the non-empty entries of requested, or us-east-1 when none are
//     given.
//
// A caller that pins a single region (e.g. --region) should pass that region as
// the sole requested entry with all == false; it then wins outright.
func ResolveRegions(ctx context.Context, cfg aws.Config, requested []string, all bool) []string {
	explicit := make([]string, 0, len(requested))
	for _, r := range requested {
		if strings.EqualFold(r, "all") {
			all = true
			continue
		}
		if r != "" {
			explicit = append(explicit, r)
		}
	}

	if all {
		return listAllRegions(ctx, cfg)
	}
	if len(explicit) == 0 {
		return []string{"us-east-1"}
	}
	return explicit
}

// describeRegionsAPI is the subset of the EC2 client used to enumerate regions,
// extracted so the fallback logic can be unit-tested with a fake.
type describeRegionsAPI interface {
	DescribeRegions(context.Context, *awsec2.DescribeRegionsInput, ...func(*awsec2.Options)) (*awsec2.DescribeRegionsOutput, error)
}

// listAllRegions resolves the full region set; overridable in tests so callers
// of ResolveRegions can exercise the all-regions branch without network access.
var listAllRegions = func(ctx context.Context, cfg aws.Config) []string {
	return regionsFromDescribe(ctx, awsec2.NewFromConfig(cfg))
}

// regionsFromDescribe lists every scannable region via ec2:DescribeRegions.
// That API is itself permission-gated: when the caller lacks ec2:DescribeRegions
// (or the call otherwise fails) it falls back to the canonical static region
// list with a warning rather than aborting, so the scan proceeds best-effort.
func regionsFromDescribe(ctx context.Context, client describeRegionsAPI) []string {
	result, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		if awserr.IsAuthError(err) {
			slog.Warn("Not authorized to call ec2:DescribeRegions; "+
				"falling back to the built-in region list",
				"regions", len(FallbackRegions))
		} else {
			slog.Warn("Unable to list AWS regions; falling back to the built-in region list",
				"error", err.Error(), "regions", len(FallbackRegions))
		}
		return FallbackRegions
	}
	regions := make([]string, 0, len(result.Regions))
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		return FallbackRegions
	}
	return regions
}

// FallbackRegions is the canonical list of standard (non opt-in-only, public
// partition) AWS regions used when ec2:DescribeRegions cannot be called — for
// example when the caller lacks that permission. It lets "--all-regions" keep
// working with a best-effort region set instead of failing outright.
var FallbackRegions = []string{
	"af-south-1",
	"ap-east-1", "ap-northeast-1", "ap-northeast-2", "ap-northeast-3",
	"ap-south-1", "ap-south-2",
	"ap-southeast-1", "ap-southeast-2", "ap-southeast-3", "ap-southeast-4",
	"ca-central-1", "ca-west-1",
	"eu-central-1", "eu-central-2",
	"eu-north-1", "eu-south-1", "eu-south-2",
	"eu-west-1", "eu-west-2", "eu-west-3",
	"il-central-1",
	"me-central-1", "me-south-1",
	"mx-central-1",
	"sa-east-1",
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
}
