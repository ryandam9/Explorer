package s3tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const awsRequestTimeout = 30 * time.Second

type S3Client struct {
	client *s3.Client
	ctx    context.Context
}

func NewS3Client(ctx context.Context, profile, region string) (*S3Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	return &S3Client{
		client: client,
		ctx:    ctx,
	}, nil
}

func (c *S3Client) requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, awsRequestTimeout)
}

func hasAPIErrorCode(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.ErrorCode() == code {
			return true
		}
	}
	return false
}

func (c *S3Client) GetBucketRegion(bucket, defaultRegion string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	output, err := c.client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		if defaultRegion != "" {
			return defaultRegion
		}
		return "us-east-1"
	}
	loc := string(output.LocationConstraint)
	if loc == "" {
		return "us-east-1"
	}
	if loc == "EU" {
		return "eu-west-1"
	}
	return loc
}

func (c *S3Client) ListBuckets() ([]s3types.Bucket, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	output, err := c.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	return output.Buckets, nil
}

type ListObjectsResult struct {
	Prefixes []s3types.CommonPrefix
	Objects  []s3types.Object
}

func (c *S3Client) ListObjects(bucket, prefix string) (*ListObjectsResult, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Delimiter: aws.String("/"),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}

	paginator := s3.NewListObjectsV2Paginator(c.client, input)
	var prefixes []s3types.CommonPrefix
	var objects []s3types.Object

	// Fetch up to 5 pages (5,000 items) to prevent hanging on massive buckets.
	// Uses explicit MaxResults to minimize round trips.
	pageCount := 0
	for paginator.HasMorePages() && pageCount < 5 {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, page.CommonPrefixes...)
		objects = append(objects, page.Contents...)
		pageCount++
	}

	return &ListObjectsResult{
		Prefixes: prefixes,
		Objects:  objects,
	}, nil
}

type ObjectDetails struct {
	ContentType string
	SSE         string
	VersionID   string
	Metadata    map[string]string
	Tags        map[string]string
}

func (c *S3Client) GetObjectDetails(bucket, key string) (*ObjectDetails, error) {
	var (
		contentType string
		sse         string
		versionID   string
		metadata    = make(map[string]string)
		tags        map[string]string
	)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		ctx, cancel := c.requestContext()
		defer cancel()

		head, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err == nil {
			if head.ContentType != nil {
				contentType = *head.ContentType
			}
			if head.ServerSideEncryption != "" {
				sse = string(head.ServerSideEncryption)
			}
			if head.VersionId != nil && *head.VersionId != "null" {
				versionID = *head.VersionId
			}
			for k, v := range head.Metadata {
				metadata[k] = v
			}
		}
	}()

	go func() {
		defer wg.Done()
		ctx, cancel := c.requestContext()
		defer cancel()

		tagging, err := c.client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err == nil {
			tags = make(map[string]string)
			for _, tag := range tagging.TagSet {
				if tag.Key != nil && tag.Value != nil {
					tags[*tag.Key] = *tag.Value
				}
			}
		}
	}()

	wg.Wait()

	if tags == nil {
		tags = make(map[string]string)
	}

	if sse == "" {
		sse = "None"
	}

	return &ObjectDetails{
		ContentType: contentType,
		SSE:         sse,
		VersionID:   versionID,
		Metadata:    metadata,
		Tags:        tags,
	}, nil
}

func (c *S3Client) FetchBucketDetails(bucket string) *BucketDetails {
	var (
		versioning        string
		encryption        = "None"
		tags              map[string]string
		policy            = "Error/Denied"
		lifecycleRules    int
		publicAccessBlock = "None"
	)

	var wg sync.WaitGroup
	wg.Add(6)

	go func() {
		defer wg.Done()
		if ver, err := c.GetBucketVersioning(bucket); err == nil {
			versioning = ver
		}
	}()

	go func() {
		defer wg.Done()
		if enc, err := c.GetBucketEncryption(bucket); err == nil && enc != nil && enc.ServerSideEncryptionConfiguration != nil && len(enc.ServerSideEncryptionConfiguration.Rules) > 0 {
			algo := string(enc.ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm)
			encryption = algo
		}
	}()

	go func() {
		defer wg.Done()
		if t, err := c.GetBucketTagging(bucket); err == nil {
			tags = t
		}
	}()

	go func() {
		defer wg.Done()
		if p, err := c.GetBucketPolicy(bucket); err == nil {
			if p == "Access Denied" {
				policy = "Access Denied"
			} else if p != "" {
				policy = "Set (Available)"
			} else {
				policy = "None"
			}
		}
	}()

	go func() {
		defer wg.Done()
		if lc, err := c.GetBucketLifecycleConfiguration(bucket); err == nil && lc != nil {
			lifecycleRules = len(lc.Rules)
		}
	}()

	go func() {
		defer wg.Done()
		if pab, err := c.GetPublicAccessBlock(bucket); err == nil && pab.PublicAccessBlockConfiguration != nil {
			config := pab.PublicAccessBlockConfiguration
			publicAccessBlock = fmt.Sprintf("BlockPublicAcls:%v IgnorePublicAcls:%v BlockPublicPolicy:%v RestrictPublicBuckets:%v",
				config.BlockPublicAcls, config.IgnorePublicAcls, config.BlockPublicPolicy, config.RestrictPublicBuckets)
		}
	}()

	wg.Wait()

	if tags == nil {
		tags = make(map[string]string)
	}

	return &BucketDetails{
		Versioning:        versioning,
		Encryption:        encryption,
		Tags:              tags,
		Policy:            policy,
		LifecycleRules:    lifecycleRules,
		PublicAccessBlock: publicAccessBlock,
	}
}

func (c *S3Client) GetObjectPreview(bucket, key string, maxBytes int64) (string, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", maxBytes-1)),
	})
	if err != nil {
		return "", err
	}
	defer out.Body.Close()

	data, err := io.ReadAll(io.LimitReader(out.Body, maxBytes))
	if err != nil {
		return "", err
	}
	for _, b := range data {
		if b == 0 {
			return "Binary object preview omitted. Use metadata/details for inspection.", nil
		}
	}
	preview := string(data)
	if int64(len(data)) == maxBytes {
		preview += "\n\n… preview truncated …"
	}
	return preview, nil
}

func (c *S3Client) GetBucketPolicy(bucket string) (string, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "NoSuchBucketPolicy") {
			return "", nil
		}
		if hasAPIErrorCode(err, "AccessDenied") {
			return "Access Denied", nil
		}
		return "", err
	}
	if out.Policy != nil {
		return *out.Policy, nil
	}
	return "", nil
}

func (c *S3Client) GetBucketAcl(bucket string) (*s3.GetBucketAclOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketVersioning(bucket string) (string, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "", err
	}
	if out.Status != "" {
		return string(out.Status), nil
	}
	return "Unversioned", nil
}

func (c *S3Client) GetBucketEncryption(bucket string) (*s3.GetBucketEncryptionOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	out, err := c.client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(bucket)})
	if err != nil && hasAPIErrorCode(err, "ServerSideEncryptionConfigurationNotFoundError") {
		return nil, nil
	}
	return out, err
}

func (c *S3Client) GetBucketTagging(bucket string) (map[string]string, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "NoSuchTagSet", "AccessDenied") {
			return map[string]string{}, nil
		}
		return nil, err
	}
	tags := make(map[string]string)
	for _, t := range out.TagSet {
		if t.Key != nil && t.Value != nil {
			tags[*t.Key] = *t.Value
		}
	}
	return tags, nil
}

func (c *S3Client) GetBucketLifecycleConfiguration(bucket string) (*s3.GetBucketLifecycleConfigurationOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	out, err := c.client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil && hasAPIErrorCode(err, "NoSuchLifecycleConfiguration", "AccessDenied") {
		return nil, nil
	}
	return out, err
}

func (c *S3Client) GetBucketReplication(bucket string) (*s3.GetBucketReplicationOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketReplication(ctx, &s3.GetBucketReplicationInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketLogging(bucket string) (*s3.GetBucketLoggingOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketLogging(ctx, &s3.GetBucketLoggingInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketMetricsConfiguration(bucket string) (*s3.ListBucketMetricsConfigurationsOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.ListBucketMetricsConfigurations(ctx, &s3.ListBucketMetricsConfigurationsInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketInventoryConfiguration(bucket string) (*s3.ListBucketInventoryConfigurationsOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.ListBucketInventoryConfigurations(ctx, &s3.ListBucketInventoryConfigurationsInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketWebsite(bucket string) (*s3.GetBucketWebsiteOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketWebsite(ctx, &s3.GetBucketWebsiteInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetPublicAccessBlock(bucket string) (*s3.GetPublicAccessBlockOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	out, err := c.client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(bucket)})
	if err != nil && hasAPIErrorCode(err, "NoSuchPublicAccessBlockConfiguration", "AccessDenied") {
		return nil, nil
	}
	return out, err
}

func (c *S3Client) GetBucketOwnershipControls(bucket string) (*s3.GetBucketOwnershipControlsOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketOwnershipControls(ctx, &s3.GetBucketOwnershipControlsInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketIntelligentTieringConfiguration(bucket string) (*s3.ListBucketIntelligentTieringConfigurationsOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.ListBucketIntelligentTieringConfigurations(ctx, &s3.ListBucketIntelligentTieringConfigurationsInput{Bucket: aws.String(bucket)})
}

func (c *S3Client) GetBucketNotificationConfiguration(bucket string) (*s3.GetBucketNotificationConfigurationOutput, error) {
	ctx, cancel := c.requestContext()
	defer cancel()
	return c.client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{Bucket: aws.String(bucket)})
}

func summarizeS3Error(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return fmt.Sprintf("%s: %s", apiErr.ErrorCode(), apiErr.ErrorMessage())
	}
	return strings.TrimSpace(err.Error())
}
