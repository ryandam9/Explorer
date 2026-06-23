package xref

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsefs "github.com/aws/aws-sdk-go-v2/service/efs"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Storage edge extractors (#339). S3 buckets are listed globally but every
// GetBucket* call must hit the bucket's own region (§3 / #323), so S3 is
// collected once, up front, with a per-bucket region client — not in the
// per-region fan-out. EFS is regional and joins collectRegion.

// --- S3 -----------------------------------------------------------------------

// s3BucketAPI is the narrow per-bucket S3 surface the edge extractor needs, so
// it can be unit-tested with a fake (the bucket-listing/region-client wiring
// stays in collectS3).
type s3BucketAPI interface {
	GetBucketNotificationConfiguration(context.Context, *awss3.GetBucketNotificationConfigurationInput, ...func(*awss3.Options)) (*awss3.GetBucketNotificationConfigurationOutput, error)
	GetBucketReplication(context.Context, *awss3.GetBucketReplicationInput, ...func(*awss3.Options)) (*awss3.GetBucketReplicationOutput, error)
	GetBucketLogging(context.Context, *awss3.GetBucketLoggingInput, ...func(*awss3.Options)) (*awss3.GetBucketLoggingOutput, error)
	GetBucketEncryption(context.Context, *awss3.GetBucketEncryptionInput, ...func(*awss3.Options)) (*awss3.GetBucketEncryptionOutput, error)
}

// collectS3 lists buckets once, filters to the scanned regions, and resolves
// each bucket's edges against a client built for the bucket's own region.
func collectS3(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, timeout time.Duration) ([]Edge, []model.ExploreError) {
	lctx, lcancel := withTimeout(ctx, timeout)
	defer lcancel()

	rec := &recorder{region: "global"}
	base := awss3.NewFromConfig(baseCfg)

	want := make(map[string]bool, len(regions))
	for _, r := range regions {
		want[r] = true
	}

	type bkt struct{ name, region string }
	var buckets []bkt
	var token *string
	for {
		out, err := base.ListBuckets(lctx, &awss3.ListBucketsInput{
			MaxBuckets:        aws.Int32(1000),
			ContinuationToken: token,
		})
		if err != nil {
			rec.record("s3", err)
			return nil, rec.errs
		}
		for _, b := range out.Buckets {
			region := aws.ToString(b.BucketRegion)
			if region == "" {
				region = "us-east-1" // location-less buckets live in us-east-1
			}
			if !want[region] {
				continue
			}
			buckets = append(buckets, bkt{name: aws.ToString(b.Name), region: region})
		}
		if out.ContinuationToken == nil || aws.ToString(out.ContinuationToken) == "" {
			break
		}
		token = out.ContinuationToken
	}

	edgesByIdx := make([][]Edge, len(buckets))
	errsByIdx := make([][]model.ExploreError, len(buckets))

	g, gctx := errgroup.WithContext(ctx)
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	g.SetLimit(maxConcurrency)
	for i, b := range buckets {
		i, b := i, b
		g.Go(func() error {
			cfg := baseCfg
			cfg.Region = b.region
			client := awss3.NewFromConfig(cfg)
			bctx, bcancel := withTimeout(gctx, timeout)
			defer bcancel()
			brec := &recorder{region: b.region}
			edgesByIdx[i] = s3BucketEdges(bctx, client, b.name, b.region, brec)
			errsByIdx[i] = brec.errs
			return nil
		})
	}
	_ = g.Wait()

	edges := []Edge{}
	errs := rec.errs
	for i := range buckets {
		edges = append(edges, edgesByIdx[i]...)
		errs = append(errs, errsByIdx[i]...)
	}
	return edges, errs
}

// s3BucketEdges emits the reference edges for one bucket: event-notification
// targets (Lambda/SNS/SQS), replication role + destination, access-log target
// bucket, and the default SSE-KMS key. Tri-state honesty (§8): an empty config
// yields no edges, while a denied/failed call is recorded so absence isn't read
// as "none" — but "not configured" errors (replication/encryption absent) are
// the expected empty case and are not recorded.
func s3BucketEdges(ctx context.Context, api s3BucketAPI, name, region string, rec *recorder) []Edge {
	from := Reference{Service: "s3", Type: "bucket", Region: region, ID: awsutil.S3BucketARN(name), Name: name}
	var edges []Edge

	if nc, err := api.GetBucketNotificationConfiguration(ctx, &awss3.GetBucketNotificationConfigurationInput{Bucket: &name}); err != nil {
		rec.record("s3", err)
	} else if nc != nil {
		for _, c := range nc.LambdaFunctionConfigurations {
			if arn := aws.ToString(c.LambdaFunctionArn); arn != "" {
				edges = append(edges, Edge{From: withVia(from, "S3 event notification → Lambda"), Target: arn})
			}
		}
		for _, c := range nc.TopicConfigurations {
			if arn := aws.ToString(c.TopicArn); arn != "" {
				edges = append(edges, Edge{From: withVia(from, "S3 event notification → SNS topic"), Target: arn})
			}
		}
		for _, c := range nc.QueueConfigurations {
			if arn := aws.ToString(c.QueueArn); arn != "" {
				edges = append(edges, Edge{From: withVia(from, "S3 event notification → SQS queue"), Target: arn})
			}
		}
	}

	if rc, err := api.GetBucketReplication(ctx, &awss3.GetBucketReplicationInput{Bucket: &name}); err != nil {
		if !isNotConfigured(err) {
			rec.record("s3", err)
		}
	} else if rc != nil && rc.ReplicationConfiguration != nil {
		if role := aws.ToString(rc.ReplicationConfiguration.Role); role != "" {
			edges = append(edges, Edge{From: withVia(from, "S3 replication role"), Target: role})
		}
		for _, rule := range rc.ReplicationConfiguration.Rules {
			if rule.Destination != nil {
				if b := aws.ToString(rule.Destination.Bucket); b != "" {
					edges = append(edges, Edge{From: withVia(from, "S3 replication destination"), Target: b})
				}
			}
		}
	}

	if lg, err := api.GetBucketLogging(ctx, &awss3.GetBucketLoggingInput{Bucket: &name}); err != nil {
		rec.record("s3", err)
	} else if lg != nil && lg.LoggingEnabled != nil {
		if tb := aws.ToString(lg.LoggingEnabled.TargetBucket); tb != "" {
			edges = append(edges, Edge{From: withVia(from, "S3 access-log target bucket"), Target: awsutil.S3BucketARN(tb)})
		}
	}

	if enc, err := api.GetBucketEncryption(ctx, &awss3.GetBucketEncryptionInput{Bucket: &name}); err != nil {
		if !isNotConfigured(err) {
			rec.record("s3", err)
		}
	} else if enc != nil && enc.ServerSideEncryptionConfiguration != nil {
		for _, r := range enc.ServerSideEncryptionConfiguration.Rules {
			if d := r.ApplyServerSideEncryptionByDefault; d != nil {
				if k := aws.ToString(d.KMSMasterKeyID); k != "" {
					edges = append(edges, Edge{From: withVia(from, "S3 default encryption key"), Target: k})
				}
			}
		}
	}

	return edges
}

// isNotConfigured reports whether err is the "this configuration is absent"
// signal some GetBucket* calls return (e.g. ReplicationConfigurationNotFound,
// ServerSideEncryptionConfigurationNotFound) — the expected empty case, not a
// failure to record.
func isNotConfigured(err error) bool {
	var ae smithy.APIError
	if errors.As(err, &ae) {
		code := ae.ErrorCode()
		return strings.Contains(code, "NotFound") || strings.HasPrefix(code, "NoSuch") || strings.Contains(code, "NotConfigured")
	}
	return false
}

// --- EFS ----------------------------------------------------------------------

// efsAPI is the narrow EFS surface the edge extractor needs (fake-testable).
type efsAPI interface {
	DescribeFileSystems(context.Context, *awsefs.DescribeFileSystemsInput, ...func(*awsefs.Options)) (*awsefs.DescribeFileSystemsOutput, error)
	DescribeMountTargets(context.Context, *awsefs.DescribeMountTargetsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetsOutput, error)
	DescribeMountTargetSecurityGroups(context.Context, *awsefs.DescribeMountTargetSecurityGroupsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetSecurityGroupsOutput, error)
}

// efsEdges builds the EFS client for the region and delegates to efsEdgesAPI.
func efsEdges(ctx context.Context, cfg aws.Config, region string, maxConcurrency int, rec *recorder) []Edge {
	return efsEdgesAPI(ctx, awsefs.NewFromConfig(cfg), region, maxConcurrency, rec)
}

// efsEdgesAPI emits file-system → KMS key and mount-target → subnet / security
// group edges across all file systems in the region.
func efsEdgesAPI(ctx context.Context, api efsAPI, region string, maxConcurrency int, rec *recorder) []Edge {
	var edges []Edge
	var marker *string
	for {
		out, err := api.DescribeFileSystems(ctx, &awsefs.DescribeFileSystemsInput{Marker: marker})
		if err != nil {
			rec.record("efs", err)
			break
		}
		for _, fs := range out.FileSystems {
			from := Reference{Service: "efs", Type: "file-system", Region: region,
				ID: aws.ToString(fs.FileSystemArn), Name: aws.ToString(fs.Name)}
			if from.ID == "" {
				from.ID = aws.ToString(fs.FileSystemId)
			}
			if k := aws.ToString(fs.KmsKeyId); k != "" {
				edges = append(edges, Edge{From: withVia(from, "EFS encryption key"), Target: k})
			}
			edges = append(edges, efsMountTargetEdges(ctx, api, from, aws.ToString(fs.FileSystemId), maxConcurrency, rec)...)
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return edges
}

func efsMountTargetEdges(ctx context.Context, api efsAPI, from Reference, fsID string, maxConcurrency int, rec *recorder) []Edge {
	type mountTarget struct{ subnetID, id string }
	var mts []mountTarget
	var marker *string
	for {
		out, err := api.DescribeMountTargets(ctx, &awsefs.DescribeMountTargetsInput{FileSystemId: &fsID, Marker: marker})
		if err != nil {
			rec.record("efs", err)
			break
		}
		for _, mt := range out.MountTargets {
			mts = append(mts, mountTarget{subnetID: aws.ToString(mt.SubnetId), id: aws.ToString(mt.MountTargetId)})
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	// One DescribeMountTargetSecurityGroups per mount target — fan out (§7).
	return boundedEdges(ctx, mts, maxConcurrency, rec, func(ctx context.Context, mt mountTarget, rec *recorder) []Edge {
		var edges []Edge
		if mt.subnetID != "" {
			edges = append(edges, Edge{From: withVia(from, "EFS mount target subnet"), Target: mt.subnetID})
		}
		if mt.id == "" {
			return edges
		}
		sgOut, err := api.DescribeMountTargetSecurityGroups(ctx, &awsefs.DescribeMountTargetSecurityGroupsInput{MountTargetId: &mt.id})
		if err != nil {
			rec.record("efs", err)
			return edges
		}
		for _, sg := range sgOut.SecurityGroups {
			edges = append(edges, Edge{From: withVia(from, "EFS mount target security group"), Target: sg})
		}
		return edges
	})
}
