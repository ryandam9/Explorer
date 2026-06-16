package awsutil

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func TestResolveRegions_Routing(t *testing.T) {
	// Replace the all-regions lister with a sentinel so the routing can be
	// asserted without any network access.
	sentinel := []string{"sentinel-all"}
	orig := listAllRegions
	listAllRegions = func(context.Context, aws.Config) []string { return sentinel }
	t.Cleanup(func() { listAllRegions = orig })

	tests := []struct {
		name      string
		requested []string
		all       bool
		want      []string
	}{
		{name: "pinned single region", requested: []string{"eu-west-1"}, want: []string{"eu-west-1"}},
		{name: "explicit config regions", requested: []string{"us-east-1", "us-west-2"}, want: []string{"us-east-1", "us-west-2"}},
		{name: "empty defaults to us-east-1", requested: nil, want: []string{"us-east-1"}},
		{name: "all flag routes to lister", all: true, want: sentinel},
		{name: "all keyword routes to lister", requested: []string{"all"}, want: sentinel},
		{name: "all keyword case-insensitive", requested: []string{"ALL"}, want: sentinel},
		{name: "empty entries dropped", requested: []string{"", "ap-south-1", ""}, want: []string{"ap-south-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRegions(context.Background(), aws.Config{}, tt.requested, tt.all)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ResolveRegions(%v, all=%v) = %v, want %v", tt.requested, tt.all, got, tt.want)
			}
		})
	}
}

type fakeDescribeRegions struct {
	out *awsec2.DescribeRegionsOutput
	err error
}

func (f fakeDescribeRegions) DescribeRegions(context.Context, *awsec2.DescribeRegionsInput, ...func(*awsec2.Options)) (*awsec2.DescribeRegionsOutput, error) {
	return f.out, f.err
}

func TestRegionsFromDescribe(t *testing.T) {
	t.Run("returns sorted region names", func(t *testing.T) {
		api := fakeDescribeRegions{out: &awsec2.DescribeRegionsOutput{Regions: []ec2types.Region{
			{RegionName: aws.String("us-west-2")},
			{RegionName: aws.String("ap-south-1")},
			{RegionName: nil}, // skipped
		}}}
		got := regionsFromDescribe(context.Background(), api)
		if want := []string{"ap-south-1", "us-west-2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("falls back when the call errors", func(t *testing.T) {
		api := fakeDescribeRegions{err: errors.New("AccessDenied: not authorized")}
		got := regionsFromDescribe(context.Background(), api)
		if !reflect.DeepEqual(got, FallbackRegions) {
			t.Errorf("expected FallbackRegions on error, got %v", got)
		}
	})

	t.Run("falls back when the result is empty", func(t *testing.T) {
		api := fakeDescribeRegions{out: &awsec2.DescribeRegionsOutput{}}
		got := regionsFromDescribe(context.Background(), api)
		if !reflect.DeepEqual(got, FallbackRegions) {
			t.Errorf("expected FallbackRegions on empty result, got %v", got)
		}
	})
}

func TestFallbackRegions(t *testing.T) {
	if len(FallbackRegions) == 0 {
		t.Fatal("FallbackRegions must not be empty")
	}

	seen := make(map[string]bool, len(FallbackRegions))
	for _, r := range FallbackRegions {
		if r == "" {
			t.Error("FallbackRegions contains an empty entry")
		}
		if seen[r] {
			t.Errorf("FallbackRegions contains duplicate %q", r)
		}
		seen[r] = true
	}

	// us-east-1 is the global/default region and must always be present so a
	// degraded "--all-regions" scan still covers it.
	if !seen["us-east-1"] {
		t.Error("FallbackRegions should include us-east-1")
	}
}
