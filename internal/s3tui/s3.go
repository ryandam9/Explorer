package s3tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

const awsRequestTimeout = 30 * time.Second

type S3Client struct {
	client *s3.Client
	ctx    context.Context
}

func NewS3Client(ctx context.Context, awsCfg *config.AWSConfig, region, endpointURL string) (*S3Client, error) {
	cfg, err := auth.BuildAWSConfig(ctx, awsCfg, region)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	var client *s3.Client
	if endpointURL != "" {
		client = s3.NewFromConfig(cfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			o.UsePathStyle = true
		})
	} else {
		client = s3.NewFromConfig(cfg)
	}

	return &S3Client{
		client: client,
		ctx:    ctx,
	}, nil
}

// ListRegions returns all available AWS regions. It queries EC2 DescribeRegions
// and falls back to the shared static list if that call fails.
func ListRegions(ctx context.Context, awsCfg *config.AWSConfig) []string {
	cfg, err := auth.BuildAWSConfig(ctx, awsCfg, "")
	if err != nil {
		return awsutil.FallbackRegions
	}
	client := awsec2.NewFromConfig(cfg)
	output, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		return awsutil.FallbackRegions
	}
	regions := make([]string, 0, len(output.Regions))
	for _, r := range output.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		return awsutil.FallbackRegions
	}
	return regions
}

// ListBucketsInRegion creates a region-scoped S3 client and returns the bucket list.
// Returns (nil, nil) for access-denied errors so callers can skip silently.
func ListBucketsInRegion(ctx context.Context, awsCfg *config.AWSConfig, region, endpointURL string) ([]s3types.Bucket, error) {
	client, err := NewS3Client(ctx, awsCfg, region, endpointURL)
	if err != nil {
		return nil, err
	}
	buckets, err := client.ListBuckets()
	if err != nil {
		if hasAPIErrorCode(err, "AccessDenied", "AccessDeniedException",
			"UnauthorizedOperation", "AuthorizationError") {
			return nil, nil
		}
		return nil, err
	}
	return buckets, nil
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
	// NextToken resumes the listing where this window stopped; nil means the
	// listing is complete. The UI surfaces it as "truncated — L loads more".
	NextToken *string
}

// listPageWindow is how many ListObjectsV2 pages (≤1,000 keys each) are
// fetched per call, keeping huge buckets responsive. Listings that hit the
// window report a continuation token instead of silently stopping.
const listPageWindow = 5

func (c *S3Client) ListObjects(bucket, prefix string, startToken *string) (*ListObjectsResult, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(bucket),
		Delimiter: aws.String("/"),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	return c.listWindow(ctx, input, startToken)
}

// ListObjectsFlat lists objects without a delimiter (flat mode, no "directories").
func (c *S3Client) ListObjectsFlat(bucket, prefix string, startToken *string) (*ListObjectsResult, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}
	return c.listWindow(ctx, input, startToken)
}

// listWindow fetches up to listPageWindow pages starting at startToken,
// reporting the continuation token when the bucket has more keys.
func (c *S3Client) listWindow(ctx context.Context, input *s3.ListObjectsV2Input, startToken *string) (*ListObjectsResult, error) {
	res := &ListObjectsResult{}
	token := startToken
	for range listPageWindow {
		input.ContinuationToken = token
		out, err := c.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, err
		}
		res.Prefixes = append(res.Prefixes, out.CommonPrefixes...)
		res.Objects = append(res.Objects, out.Contents...)
		if !aws.ToBool(out.IsTruncated) {
			return res, nil
		}
		token = out.NextContinuationToken
	}
	res.NextToken = token
	return res, nil
}

type ObjectDetails struct {
	ContentType        string
	SSE                string
	VersionID          string
	Metadata           map[string]string
	Tags               map[string]string
	ContentEncoding    string
	ContentDisposition string
	CacheControl       string
	KMSKeyID           string
	StorageClass       string
	RestoreStatus      string
	ACLGrants          string
	Retention          string
	LegalHold          string
}

func (c *S3Client) GetObjectDetails(bucket, key string) (*ObjectDetails, error) {
	var (
		contentType        string
		sse                string
		versionID          string
		metadata           = make(map[string]string)
		tags               map[string]string
		contentEncoding    string
		contentDisposition string
		cacheControl       string
		kmsKeyID           string
		storageClass       string
		restoreStatus      string
		aclGrants          string
		retention          string
		legalHold          string
		headErr            error
	)

	var wg sync.WaitGroup
	var errMu sync.Mutex
	wg.Add(4)

	go func() {
		defer wg.Done()
		ctx, cancel := c.requestContext()
		defer cancel()

		head, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			errMu.Lock()
			headErr = err
			errMu.Unlock()
			return
		}
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
		if head.ContentEncoding != nil {
			contentEncoding = *head.ContentEncoding
		}
		if head.ContentDisposition != nil {
			contentDisposition = *head.ContentDisposition
		}
		if head.CacheControl != nil {
			cacheControl = *head.CacheControl
		}
		if head.SSEKMSKeyId != nil {
			kmsKeyID = *head.SSEKMSKeyId
		}
		if head.StorageClass != "" {
			storageClass = string(head.StorageClass)
		}
		if head.Restore != nil {
			restoreStatus = *head.Restore
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

	go func() {
		defer wg.Done()
		ctx, cancel := c.requestContext()
		defer cancel()

		aclOut, err := c.client.GetObjectAcl(ctx, &s3.GetObjectAclInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			if hasAPIErrorCode(err, "AccessDenied") {
				aclGrants = "Access Denied"
			}
			return
		}
		var grants []string
		for _, g := range aclOut.Grants {
			grantee := "Unknown"
			if g.Grantee != nil {
				if g.Grantee.DisplayName != nil {
					grantee = *g.Grantee.DisplayName
				} else if g.Grantee.URI != nil {
					uri := *g.Grantee.URI
					// Shorten well-known URIs
					if strings.Contains(uri, "AllUsers") {
						grantee = "AllUsers"
					} else if strings.Contains(uri, "AuthenticatedUsers") {
						grantee = "AuthenticatedUsers"
					} else {
						grantee = uri
					}
				} else if g.Grantee.ID != nil {
					id := *g.Grantee.ID
					if len(id) > 12 {
						id = id[:12] + "…"
					}
					grantee = id
				}
			}
			grants = append(grants, fmt.Sprintf("%s: %s", grantee, string(g.Permission)))
		}
		if len(grants) > 0 {
			aclGrants = strings.Join(grants, ", ")
		} else {
			aclGrants = "None"
		}
	}()

	go func() {
		defer wg.Done()

		// Object Retention
		ctx1, cancel1 := c.requestContext()
		defer cancel1()
		retOut, err := c.client.GetObjectRetention(ctx1, &s3.GetObjectRetentionInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			if hasAPIErrorCode(err, "ObjectLockConfigurationNotFoundError", "NoSuchObjectLockConfiguration") ||
				strings.Contains(err.Error(), "lock") || strings.Contains(err.Error(), "Lock") {
				retention = "Not set"
			} else {
				retention = "Not set"
			}
		} else if retOut.Retention != nil {
			mode := string(retOut.Retention.Mode)
			until := ""
			if retOut.Retention.RetainUntilDate != nil {
				until = retOut.Retention.RetainUntilDate.Format("2006-01-02")
			}
			retention = fmt.Sprintf("Mode: %s, Until: %s", mode, until)
		} else {
			retention = "Not set"
		}

		// Object Legal Hold
		ctx2, cancel2 := c.requestContext()
		defer cancel2()
		holdOut, err := c.client.GetObjectLegalHold(ctx2, &s3.GetObjectLegalHoldInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			legalHold = "Not set"
		} else if holdOut.LegalHold != nil {
			legalHold = string(holdOut.LegalHold.Status)
		} else {
			legalHold = "Not set"
		}
	}()

	wg.Wait()

	if headErr != nil {
		return nil, fmt.Errorf("HeadObject: %w", headErr)
	}

	if tags == nil {
		tags = make(map[string]string)
	}

	if sse == "" {
		sse = "None"
	}

	return &ObjectDetails{
		ContentType:        contentType,
		SSE:                sse,
		VersionID:          versionID,
		Metadata:           metadata,
		Tags:               tags,
		ContentEncoding:    contentEncoding,
		ContentDisposition: contentDisposition,
		CacheControl:       cacheControl,
		KMSKeyID:           kmsKeyID,
		StorageClass:       storageClass,
		RestoreStatus:      restoreStatus,
		ACLGrants:          aclGrants,
		Retention:          retention,
		LegalHold:          legalHold,
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
		aclSummary        string
		ownershipControls string
		policyStatus      string
		cors              string
		website           string
		logging           string
		notifications     string
		requestPayment    string
		acceleration      string
		objectLock        string
		replication       string
		multipartUploads  int
		intelligentTier   string
	)

	var wg sync.WaitGroup
	wg.Add(19)

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
		if pab, err := c.GetPublicAccessBlock(bucket); err == nil && pab != nil && pab.PublicAccessBlockConfiguration != nil {
			config := pab.PublicAccessBlockConfiguration
			publicAccessBlock = fmt.Sprintf("BlockPublicAcls:%v IgnorePublicAcls:%v BlockPublicPolicy:%v RestrictPublicBuckets:%v",
				config.BlockPublicAcls, config.IgnorePublicAcls, config.BlockPublicPolicy, config.RestrictPublicBuckets)
		}
	}()

	go func() {
		defer wg.Done()
		aclSummary = c.getBucketACLSummary(bucket)
	}()

	go func() {
		defer wg.Done()
		ownershipControls = c.getBucketOwnershipControls(bucket)
	}()

	go func() {
		defer wg.Done()
		policyStatus = c.getBucketPolicyStatus(bucket)
	}()

	go func() {
		defer wg.Done()
		cors = c.getBucketCORS(bucket)
	}()

	go func() {
		defer wg.Done()
		website = c.getBucketWebsite(bucket)
	}()

	go func() {
		defer wg.Done()
		logging = c.getBucketLogging(bucket)
	}()

	go func() {
		defer wg.Done()
		notifications = c.getBucketNotifications(bucket)
	}()

	go func() {
		defer wg.Done()
		requestPayment = c.getBucketRequestPayment(bucket)
	}()

	go func() {
		defer wg.Done()
		acceleration = c.getBucketAcceleration(bucket)
	}()

	go func() {
		defer wg.Done()
		objectLock = c.getObjectLockConfig(bucket)
	}()

	go func() {
		defer wg.Done()
		replication = c.getBucketReplication(bucket)
	}()

	go func() {
		defer wg.Done()
		multipartUploads = c.countMultipartUploads(bucket)
	}()

	go func() {
		defer wg.Done()
		intelligentTier = c.getIntelligentTiering(bucket)
	}()

	wg.Wait()

	if tags == nil {
		tags = make(map[string]string)
	}

	return &BucketDetails{
		Versioning:         versioning,
		Encryption:         encryption,
		Tags:               tags,
		Policy:             policy,
		LifecycleRules:     lifecycleRules,
		PublicAccessBlock:  publicAccessBlock,
		ACLSummary:         aclSummary,
		OwnershipControls:  ownershipControls,
		PolicyStatus:       policyStatus,
		CORS:               cors,
		Website:            website,
		Logging:            logging,
		Notifications:      notifications,
		RequestPayment:     requestPayment,
		Acceleration:       acceleration,
		ObjectLock:         objectLock,
		Replication:        replication,
		MultipartUploads:   multipartUploads,
		IntelligentTiering: intelligentTier,
	}
}

func (c *S3Client) getBucketACLSummary(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "AccessDenied") {
			return "Access Denied"
		}
		return "—"
	}

	var grants []string
	// Owner always has FULL_CONTROL
	if out.Owner != nil && out.Owner.DisplayName != nil {
		grants = append(grants, fmt.Sprintf("Owner: FULL_CONTROL"))
	}
	for _, g := range out.Grants {
		if g.Grantee == nil {
			continue
		}
		grantee := "Unknown"
		if g.Grantee.URI != nil {
			uri := *g.Grantee.URI
			if strings.Contains(uri, "AllUsers") {
				grantee = "AllUsers"
			} else if strings.Contains(uri, "AuthenticatedUsers") {
				grantee = "AuthenticatedUsers"
			} else {
				grantee = uri
			}
		} else if g.Grantee.DisplayName != nil {
			grantee = *g.Grantee.DisplayName
		}
		grants = append(grants, fmt.Sprintf("%s: %s", grantee, string(g.Permission)))
	}

	if len(grants) == 0 {
		return "None"
	}
	return strings.Join(grants, ", ")
}

func (c *S3Client) getBucketOwnershipControls(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketOwnershipControls(ctx, &s3.GetBucketOwnershipControlsInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "OwnershipControlsNotFoundError") {
			return "Not set"
		}
		return "—"
	}
	if out.OwnershipControls != nil && len(out.OwnershipControls.Rules) > 0 {
		return string(out.OwnershipControls.Rules[0].ObjectOwnership)
	}
	return "Not set"
}

func (c *S3Client) getBucketCORS(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "NoSuchCORSConfiguration") {
			return "Not configured"
		}
		return "—"
	}
	return fmt.Sprintf("%d rule(s)", len(out.CORSRules))
}

func (c *S3Client) getBucketWebsite(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketWebsite(ctx, &s3.GetBucketWebsiteInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "NoSuchWebsiteConfiguration") {
			return "Not configured"
		}
		return "Not configured"
	}
	if out.IndexDocument != nil && out.IndexDocument.Suffix != nil {
		return fmt.Sprintf("index: %s", *out.IndexDocument.Suffix)
	}
	return "Configured"
}

func (c *S3Client) getBucketLogging(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketLogging(ctx, &s3.GetBucketLoggingInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "—"
	}
	if out.LoggingEnabled != nil && out.LoggingEnabled.TargetBucket != nil {
		prefix := ""
		if out.LoggingEnabled.TargetPrefix != nil {
			prefix = *out.LoggingEnabled.TargetPrefix
		}
		return fmt.Sprintf("→ s3://%s/%s", *out.LoggingEnabled.TargetBucket, prefix)
	}
	return "Disabled"
}

func (c *S3Client) getBucketNotifications(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketNotificationConfiguration(ctx, &s3.GetBucketNotificationConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "—"
	}

	lambdaCount := len(out.LambdaFunctionConfigurations)
	sqsCount := len(out.QueueConfigurations)
	snsCount := len(out.TopicConfigurations)

	var parts []string
	if lambdaCount > 0 {
		parts = append(parts, fmt.Sprintf("%d Lambda", lambdaCount))
	}
	if sqsCount > 0 {
		parts = append(parts, fmt.Sprintf("%d SQS", sqsCount))
	}
	if snsCount > 0 {
		parts = append(parts, fmt.Sprintf("%d SNS", snsCount))
	}

	if len(parts) == 0 {
		return "None"
	}
	return strings.Join(parts, ", ")
}

func (c *S3Client) getBucketRequestPayment(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketRequestPayment(ctx, &s3.GetBucketRequestPaymentInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "—"
	}
	return string(out.Payer)
}

func (c *S3Client) getBucketAcceleration(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketAccelerateConfiguration(ctx, &s3.GetBucketAccelerateConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "AccessDenied", "MethodNotAllowed") {
			return "Not supported"
		}
		return "—"
	}
	if out.Status == "" {
		return "Not enabled"
	}
	return string(out.Status)
}

func (c *S3Client) getObjectLockConfig(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetObjectLockConfiguration(ctx, &s3.GetObjectLockConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "ObjectLockConfigurationNotFoundError") {
			return "Not configured"
		}
		return "—"
	}
	if out.ObjectLockConfiguration == nil {
		return "Not configured"
	}
	cfg := out.ObjectLockConfiguration
	status := string(cfg.ObjectLockEnabled)
	if cfg.Rule != nil && cfg.Rule.DefaultRetention != nil {
		dr := cfg.Rule.DefaultRetention
		mode := string(dr.Mode)
		if dr.Days != nil {
			return fmt.Sprintf("%s, Mode: %s, %d days", status, mode, *dr.Days)
		}
		if dr.Years != nil {
			return fmt.Sprintf("%s, Mode: %s, %d years", status, mode, *dr.Years)
		}
		return fmt.Sprintf("%s, Mode: %s", status, mode)
	}
	return status
}

func (c *S3Client) countMultipartUploads(bucket string) int {
	ctx, cancel := c.requestContext()
	defer cancel()

	var total int
	input := &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)}
	for {
		out, err := c.client.ListMultipartUploads(ctx, input)
		if err != nil {
			return total
		}
		total += len(out.Uploads)
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		input.KeyMarker = out.NextKeyMarker
		input.UploadIdMarker = out.NextUploadIdMarker
	}
	return total
}

func (c *S3Client) getBucketReplication(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketReplication(ctx, &s3.GetBucketReplicationInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "ReplicationConfigurationNotFoundError") {
			return "Not configured"
		}
		return "Not configured"
	}
	if out.ReplicationConfiguration == nil {
		return "Not configured"
	}
	total := len(out.ReplicationConfiguration.Rules)
	enabled := 0
	for _, r := range out.ReplicationConfiguration.Rules {
		if r.Status == s3types.ReplicationRuleStatusEnabled {
			enabled++
		}
	}
	return fmt.Sprintf("%d rules (%d enabled)", total, enabled)
}

func (c *S3Client) getIntelligentTiering(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.ListBucketIntelligentTieringConfigurations(ctx, &s3.ListBucketIntelligentTieringConfigurationsInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "None"
	}
	count := len(out.IntelligentTieringConfigurationList)
	if count == 0 {
		return "None"
	}
	return fmt.Sprintf("%d config(s)", count)
}

func (c *S3Client) getBucketPolicyStatus(bucket string) string {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.client.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{Bucket: aws.String(bucket)})
	if err != nil {
		if hasAPIErrorCode(err, "NoSuchBucketPolicy") {
			return "No policy"
		}
		return "—"
	}
	if out.PolicyStatus != nil && out.PolicyStatus.IsPublic != nil && *out.PolicyStatus.IsPublic {
		return "Public"
	}
	return "Not public"
}

// PresignGetObject returns a presigned URL for a GET request valid for ttl duration.
func (c *S3Client) PresignGetObject(bucket, key string, ttl time.Duration) (string, error) {
	presigner := s3.NewPresignClient(c.client)
	ctx, cancel := c.requestContext()
	defer cancel()

	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// DownloadProgress tracks the byte progress of an in-flight download. It is
// safe for concurrent use: the download goroutine updates it while the TUI
// reads it from the Bubble Tea update loop.
type DownloadProgress struct {
	total   atomic.Int64
	written atomic.Int64
}

// Write implements io.Writer so the type can be used as a TeeReader sink; it
// only counts bytes and never stores them.
func (p *DownloadProgress) Write(b []byte) (int, error) {
	p.written.Add(int64(len(b)))
	return len(b), nil
}

// SetTotal records the total object size (from ContentLength).
func (p *DownloadProgress) SetTotal(n int64) { p.total.Store(n) }

// Total returns the total object size, or 0 if not yet known.
func (p *DownloadProgress) Total() int64 { return p.total.Load() }

// Written returns the number of bytes downloaded so far.
func (p *DownloadProgress) Written() int64 { return p.written.Load() }

// DownloadObject downloads bucket/key to localPath, streaming the body. If
// progress is non-nil it is updated with the object size and bytes written as
// the transfer proceeds.
func (c *S3Client) DownloadObject(bucket, key, localPath string, progress *DownloadProgress) error {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Minute)
	defer cancel()

	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("GetObject: %w", err)
	}
	defer out.Body.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}

	var src io.Reader = out.Body
	if progress != nil {
		if out.ContentLength != nil {
			progress.SetTotal(*out.ContentLength)
		}
		src = io.TeeReader(out.Body, progress)
	}

	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		os.Remove(localPath)
		return fmt.Errorf("write file: %w", err)
	}
	return f.Close()
}

// DeleteObject deletes bucket/key.
func (c *S3Client) DeleteObject(bucket, key string) error {
	ctx, cancel := c.requestContext()
	defer cancel()

	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
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
