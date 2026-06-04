package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

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
	// S3 ListBuckets is global.
	// To avoid listing them multiple times, we might check if region is us-east-1, but for now we just collect.
	client := s3.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list S3 buckets: %w", err)
	}

	for _, bucket := range output.Buckets {
		name := aws.ToString(bucket.Name)

		res := model.Resource{
			Service: "s3",
			Type:    "bucket",
			Region:  "global", // Buckets are global in listing
			ID:      name,
			Name:    name,
			Summary: map[string]string{
				"creationDate": "",
			},
		}

		if bucket.CreationDate != nil {
			res.CreatedAt = bucket.CreationDate
			res.Summary["creationDate"] = bucket.CreationDate.Format("2006-01-02 15:04:05")
		}

		// Optionally get bucket location
		if input.DetailLevel == services.DetailLevelDetailed || input.DetailLevel == services.DetailLevelRaw {
			details := make(map[string]interface{})

			loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: &name})
			if err == nil {
				details["locationConstraint"] = string(loc.LocationConstraint)
			}

			versioning, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: &name})
			if err == nil && versioning.Status != "" {
				details["versioningStatus"] = string(versioning.Status)
			}

			encryption, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: &name})
			if err == nil {
				details["encryption"] = encryption.ServerSideEncryptionConfiguration
			}

			tags, err := client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: &name})
			if err == nil {
				details["tags"] = tags.TagSet
			}

			acl, err := client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: &name})
			if err == nil {
				details["acl"] = acl.Grants
			}

			policy, err := client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: &name})
			if err == nil && policy.Policy != nil {
				details["policy"] = *policy.Policy
			}

			lifecycle, err := client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: &name})
			if err == nil {
				details["lifecycle"] = lifecycle.Rules
			}

			pab, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: &name})
			if err == nil {
				details["publicAccessBlock"] = pab.PublicAccessBlockConfiguration
			}

			res.Details = details
		}

		resources = append(resources, res)
	}

	return resources, nil
}
