package s3

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"

	"github.com/user/aws_explorer/internal/awsutil"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

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

	paginator := s3.NewListBucketsPaginator(client, &s3.ListBucketsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list S3 buckets: %w", err)
		}

		for _, bucket := range page.Buckets {
			name := aws.ToString(bucket.Name)

			res := model.Resource{
				Service: "s3",
				Type:    "bucket",
				Region:  "global",
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

			resources = append(resources, res)
		}
	}

	// Detail fetching makes ~8 API calls per bucket; run buckets concurrently
	// (bounded) instead of one bucket at a time.
	if input.DetailLevel == services.DetailLevelDetailed || input.DetailLevel == services.DetailLevelRaw {
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(bucketDetailConcurrency)
		for i := range resources {
			g.Go(func() error {
				resources[i].Details = fetchBucketDetails(gctx, client, resources[i].Name)
				return nil
			})
		}
		_ = g.Wait() // detail fetches swallow per-call errors; nothing to return
	}

	return resources, nil
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
				return nil, false
			}
			return string(loc.LocationConstraint), true
		}},
		{"versioningStatus", func() (any, bool) {
			v, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &name})
			if err != nil || v.Status == "" {
				return nil, false
			}
			return string(v.Status), true
		}},
		{"encryption", func() (any, bool) {
			enc, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: &name})
			if err != nil {
				return nil, false
			}
			return enc.ServerSideEncryptionConfiguration, true
		}},
		{"tags", func() (any, bool) {
			tags, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: &name})
			if err != nil {
				return nil, false
			}
			return tags.TagSet, true
		}},
		{"acl", func() (any, bool) {
			acl, err := client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: &name})
			if err != nil {
				return nil, false
			}
			return acl.Grants, true
		}},
		{"policy", func() (any, bool) {
			pol, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: &name})
			if err != nil || pol.Policy == nil {
				return nil, false
			}
			return *pol.Policy, true
		}},
		{"lifecycle", func() (any, bool) {
			lc, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: &name})
			if err != nil {
				return nil, false
			}
			return lc.Rules, true
		}},
		{"publicAccessBlock", func() (any, bool) {
			pab, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: &name})
			if err != nil {
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
