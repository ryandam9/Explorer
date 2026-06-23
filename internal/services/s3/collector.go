package s3

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// warnBucketDetail logs a failed bucket sub-resource read so a swallowed error
// is at least traceable (CLAUDE.md §6a) — the detail key is still omitted, but
// the failure is no longer invisible.
func warnBucketDetail(field, bucket string, err error) {
	slog.Warn("s3: bucket detail read failed", "field", field, "bucket", bucket, "err", err)
}

// warnBucketDetailUnlessNotFound is warnBucketDetail except it stays silent for
// the SDK codes that genuinely mean "this configuration is unset" (so the key is
// correctly absent rather than failed). Keeps "not set" distinct from "failed"
// (CLAUDE.md §8).
func warnBucketDetailUnlessNotFound(field, bucket string, err error, notFoundCodes ...string) {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		for _, c := range notFoundCodes {
			if apiErr.ErrorCode() == c {
				return
			}
		}
	}
	warnBucketDetail(field, bucket, err)
}

// bucketDetailConcurrency bounds how many buckets have their details fetched
// at once. Each bucket already fans out ~8 API calls internally, so this
// keeps total in-flight requests at a moderate level (~32).
const bucketDetailConcurrency = 4

// Collector implements the services.Collector interface for S3.
type Collector struct{}

// NewCollector returns a new S3 Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "s3"
}

func (c *Collector) IsGlobal() bool {
	return true
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := s3.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	// At detailed levels the resources are enriched in place after listing,
	// so pages can only be streamed out when no enrichment follows.
	wantDetails := input.DetailLevel == services.DetailLevelDetailed || input.DetailLevel == services.DetailLevelRaw

	// MaxBuckets is set to the API maximum so each bucket's BucketRegion is
	// returned: S3 only includes the per-bucket region when the request carries
	// at least one valid parameter. Pagination still walks every bucket.
	paginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{
		MaxBuckets: aws.Int32(10000),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list S3 buckets: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Buckets))
		for _, bucket := range page.Buckets {
			batch = append(batch, mapBucket(bucket))
		}
		if wantDetails {
			resources = append(resources, batch...)
		} else {
			resources = input.EmitOrAppend(resources, batch)
		}
	}

	// Detail fetching makes ~8 API calls per bucket; run buckets concurrently
	// (bounded) instead of one bucket at a time.
	if wantDetails {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(bucketDetailConcurrency)
		for i := range resources {
			g.Go(func() error {
				resources[i].Details = fetchBucketDetails(gctx, client, resources[i].Name)
				return nil
			})
		}
		_ = g.Wait() // per-bucket detail failures are logged (warnBucketDetail), not returned
	}

	return resources, nil
}

// mapBucket builds a Resource from a ListBuckets entry. The bucket's home
// region is taken from BucketRegion (returned by ListBuckets) rather than the
// global label; an absent region falls back to "global".
func mapBucket(bucket types.Bucket) model.Resource {
	name := aws.ToString(bucket.Name)

	region := aws.ToString(bucket.BucketRegion)
	if region == "" {
		region = "global"
	}

	res := model.Resource{
		Service: "s3",
		Type:    "bucket",
		Region:  region,
		ID:      name,
		Name:    name,
		ARN:     awsutil.S3BucketARN(name),
		Summary: map[string]string{
			"creationDate": "",
		},
	}

	if bucket.CreationDate != nil {
		res.CreatedAt = bucket.CreationDate
		res.Summary["creationDate"] = bucket.CreationDate.Format("2006-01-02 15:04:05")
	}

	return res
}

// fetchBucketDetails fetches all bucket detail fields concurrently.
func fetchBucketDetails(ctx context.Context, client *s3.Client, name string) map[string]any {
	details := make(map[string]any)
	var mu sync.Mutex

	type fetch struct {
		key string
		fn  func() (any, bool)
	}

	fetches := []fetch{
		{"locationConstraint", func() (any, bool) {
			loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: &name})
			if err != nil {
				warnBucketDetail("location", name, err)
				return nil, false
			}
			return string(loc.LocationConstraint), true
		}},
		{"versioningStatus", func() (any, bool) {
			v, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &name})
			if err != nil {
				warnBucketDetail("versioning", name, err)
				return nil, false
			}
			if v.Status == "" {
				return nil, false // genuinely never enabled
			}
			return string(v.Status), true
		}},
		{"encryption", func() (any, bool) {
			enc, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: &name})
			if err != nil {
				warnBucketDetailUnlessNotFound("encryption", name, err, "ServerSideEncryptionConfigurationNotFoundError")
				return nil, false
			}
			return enc.ServerSideEncryptionConfiguration, true
		}},
		{"tags", func() (any, bool) {
			tags, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: &name})
			if err != nil {
				warnBucketDetailUnlessNotFound("tags", name, err, "NoSuchTagSet")
				return nil, false
			}
			return tags.TagSet, true
		}},
		{"acl", func() (any, bool) {
			acl, err := client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: &name})
			if err != nil {
				warnBucketDetail("acl", name, err)
				return nil, false
			}
			return acl.Grants, true
		}},
		{"policy", func() (any, bool) {
			pol, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: &name})
			if err != nil {
				warnBucketDetailUnlessNotFound("policy", name, err, "NoSuchBucketPolicy")
				return nil, false
			}
			if pol.Policy == nil {
				return nil, false
			}
			return *pol.Policy, true
		}},
		{"lifecycle", func() (any, bool) {
			lc, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: &name})
			if err != nil {
				warnBucketDetailUnlessNotFound("lifecycle", name, err, "NoSuchLifecycleConfiguration")
				return nil, false
			}
			return lc.Rules, true
		}},
		{"publicAccessBlock", func() (any, bool) {
			pab, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: &name})
			if err != nil {
				warnBucketDetailUnlessNotFound("publicAccessBlock", name, err, "NoSuchPublicAccessBlockConfiguration")
				return nil, false
			}
			return pab.PublicAccessBlockConfiguration, true
		}},
	}

	var wg sync.WaitGroup
	for _, f := range fetches {
		wg.Add(1)
		go func(f fetch) {
			defer wg.Done()
			val, ok := f.fn()
			if ok {
				mu.Lock()
				details[f.key] = val
				mu.Unlock()
			}
		}(f)
	}
	wg.Wait()

	return details
}
