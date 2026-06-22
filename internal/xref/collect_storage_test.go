package xref

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsefs "github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithy "github.com/aws/smithy-go"
)

// --- S3 -----------------------------------------------------------------------

type fakeS3 struct {
	notif    *awss3.GetBucketNotificationConfigurationOutput
	notifErr error
	repl     *awss3.GetBucketReplicationOutput
	replErr  error
	log      *awss3.GetBucketLoggingOutput
	logErr   error
	enc      *awss3.GetBucketEncryptionOutput
	encErr   error
}

func (f *fakeS3) GetBucketNotificationConfiguration(context.Context, *awss3.GetBucketNotificationConfigurationInput, ...func(*awss3.Options)) (*awss3.GetBucketNotificationConfigurationOutput, error) {
	return f.notif, f.notifErr
}
func (f *fakeS3) GetBucketReplication(context.Context, *awss3.GetBucketReplicationInput, ...func(*awss3.Options)) (*awss3.GetBucketReplicationOutput, error) {
	return f.repl, f.replErr
}
func (f *fakeS3) GetBucketLogging(context.Context, *awss3.GetBucketLoggingInput, ...func(*awss3.Options)) (*awss3.GetBucketLoggingOutput, error) {
	return f.log, f.logErr
}
func (f *fakeS3) GetBucketEncryption(context.Context, *awss3.GetBucketEncryptionInput, ...func(*awss3.Options)) (*awss3.GetBucketEncryptionOutput, error) {
	return f.enc, f.encErr
}

func TestS3BucketEdges_AllSources(t *testing.T) {
	fake := &fakeS3{
		notif: &awss3.GetBucketNotificationConfigurationOutput{
			LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{
				{LambdaFunctionArn: aws.String("arn:aws:lambda:us-east-1:111:function:thumb")},
			},
			TopicConfigurations: []s3types.TopicConfiguration{
				{TopicArn: aws.String("arn:aws:sns:us-east-1:111:uploads")},
			},
			QueueConfigurations: []s3types.QueueConfiguration{
				{QueueArn: aws.String("arn:aws:sqs:us-east-1:111:ingest")},
			},
		},
		repl: &awss3.GetBucketReplicationOutput{
			ReplicationConfiguration: &s3types.ReplicationConfiguration{
				Role: aws.String("arn:aws:iam::111:role/replication"),
				Rules: []s3types.ReplicationRule{
					{Destination: &s3types.Destination{Bucket: aws.String("arn:aws:s3:::dest-bucket")}},
				},
			},
		},
		log: &awss3.GetBucketLoggingOutput{
			LoggingEnabled: &s3types.LoggingEnabled{TargetBucket: aws.String("logs-bucket")},
		},
		enc: &awss3.GetBucketEncryptionOutput{
			ServerSideEncryptionConfiguration: &s3types.ServerSideEncryptionConfiguration{
				Rules: []s3types.ServerSideEncryptionRule{
					{ApplyServerSideEncryptionByDefault: &s3types.ServerSideEncryptionByDefault{
						KMSMasterKeyID: aws.String("arn:aws:kms:us-east-1:111:key/abc"),
					}},
				},
			},
		},
	}
	rec := &recorder{region: "us-east-1"}
	edges := s3BucketEdges(context.Background(), fake, "media", "us-east-1", rec)

	if len(rec.errs) != 0 {
		t.Fatalf("unexpected errors: %+v", rec.errs)
	}
	want := map[string]string{
		"arn:aws:lambda:us-east-1:111:function:thumb": "S3 event notification → Lambda",
		"arn:aws:sns:us-east-1:111:uploads":           "S3 event notification → SNS topic",
		"arn:aws:sqs:us-east-1:111:ingest":            "S3 event notification → SQS queue",
		"arn:aws:iam::111:role/replication":           "S3 replication role",
		"arn:aws:s3:::dest-bucket":                    "S3 replication destination",
		"arn:aws:s3:::logs-bucket":                    "S3 access-log target bucket",
		"arn:aws:kms:us-east-1:111:key/abc":           "S3 default encryption key",
	}
	if len(edges) != len(want) {
		t.Fatalf("want %d edges, got %d: %+v", len(want), len(edges), edges)
	}
	for _, e := range edges {
		via, ok := want[e.Target]
		if !ok {
			t.Errorf("unexpected edge target %q", e.Target)
			continue
		}
		if e.From.Via != via {
			t.Errorf("target %q via = %q, want %q", e.Target, e.From.Via, via)
		}
		if e.From.Service != "s3" || e.From.ID != "arn:aws:s3:::media" {
			t.Errorf("from = %+v", e.From)
		}
	}
}

func TestS3BucketEdges_TriStateErrors(t *testing.T) {
	notConfigured := &smithy.GenericAPIError{Code: "ReplicationConfigurationNotFoundError", Message: "none"}
	denied := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}

	// Replication "not configured" is the expected empty case → not recorded.
	rec := &recorder{region: "us-east-1"}
	edges := s3BucketEdges(context.Background(), &fakeS3{
		notif:   &awss3.GetBucketNotificationConfigurationOutput{},
		replErr: notConfigured,
		log:     &awss3.GetBucketLoggingOutput{},
		encErr:  notConfigured,
	}, "b", "us-east-1", rec)
	if len(edges) != 0 || len(rec.errs) != 0 {
		t.Fatalf("not-configured should yield no edges and no errors: edges=%+v errs=%+v", edges, rec.errs)
	}

	// A genuine denial is recorded (so absence isn't read as "none", §6a/§8).
	rec = &recorder{region: "us-east-1"}
	_ = s3BucketEdges(context.Background(), &fakeS3{
		notifErr: denied,
		replErr:  notConfigured,
		log:      &awss3.GetBucketLoggingOutput{},
		encErr:   notConfigured,
	}, "b", "us-east-1", rec)
	if len(rec.errs) != 1 {
		t.Fatalf("denied notification read should record 1 error, got %+v", rec.errs)
	}
}

func TestS3BucketEdge_ResolvesBothDirections(t *testing.T) {
	rec := &recorder{region: "us-east-1"}
	edges := s3BucketEdges(context.Background(), &fakeS3{
		notif: &awss3.GetBucketNotificationConfigurationOutput{
			LambdaFunctionConfigurations: []s3types.LambdaFunctionConfiguration{
				{LambdaFunctionArn: aws.String("arn:aws:lambda:us-east-1:111:function:thumb")},
			},
		},
	}, "media", "us-east-1", rec)

	fwd := BuildForwardIndex(edges)
	rev := BuildIndex(edges)

	// related(bucket).Uses → the Lambda
	bucket := Related("arn:aws:s3:::media", fwd, rev, 1)
	if len(bucket.Uses) != 1 || bucket.Uses[0].Service != "lambda" {
		t.Fatalf("bucket.Uses = %+v", bucket.Uses)
	}
	// related(lambda).UsedBy → the bucket
	lam := Related("arn:aws:lambda:us-east-1:111:function:thumb", fwd, rev, 1)
	if len(lam.UsedBy) != 1 || lam.UsedBy[0].Service != "s3" {
		t.Fatalf("lambda.UsedBy = %+v", lam.UsedBy)
	}
	if lam.UsedBy[0].Via != "S3 event notification → Lambda" {
		t.Errorf("via = %q", lam.UsedBy[0].Via)
	}
}

// --- EBS ----------------------------------------------------------------------

func TestEBSVolumeEdges(t *testing.T) {
	vols := []ec2types.Volume{
		{
			VolumeId:    aws.String("vol-1"),
			KmsKeyId:    aws.String("arn:aws:kms:us-east-1:111:key/k"),
			Attachments: []ec2types.VolumeAttachment{{InstanceId: aws.String("i-123")}},
		},
		{VolumeId: aws.String("vol-2")}, // unencrypted, unattached → no edges
	}
	edges := ebsVolumeEdges(vols, "us-east-1")
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(edges), edges)
	}
	got := map[string]string{}
	for _, e := range edges {
		got[e.From.Via] = e.Target
	}
	if got["volume encryption key"] != "arn:aws:kms:us-east-1:111:key/k" {
		t.Errorf("kms edge = %+v", got)
	}
	if got["attached to instance"] != "i-123" {
		t.Errorf("attachment edge = %+v", got)
	}
}

// --- EFS ----------------------------------------------------------------------

type fakeEFS struct {
	fs     *awsefs.DescribeFileSystemsOutput
	fsErr  error
	mts    *awsefs.DescribeMountTargetsOutput
	mtErr  error
	sgs    *awsefs.DescribeMountTargetSecurityGroupsOutput
	sgsErr error
}

func (f *fakeEFS) DescribeFileSystems(context.Context, *awsefs.DescribeFileSystemsInput, ...func(*awsefs.Options)) (*awsefs.DescribeFileSystemsOutput, error) {
	return f.fs, f.fsErr
}
func (f *fakeEFS) DescribeMountTargets(context.Context, *awsefs.DescribeMountTargetsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetsOutput, error) {
	return f.mts, f.mtErr
}
func (f *fakeEFS) DescribeMountTargetSecurityGroups(context.Context, *awsefs.DescribeMountTargetSecurityGroupsInput, ...func(*awsefs.Options)) (*awsefs.DescribeMountTargetSecurityGroupsOutput, error) {
	return f.sgs, f.sgsErr
}

func TestEFSEdges(t *testing.T) {
	fake := &fakeEFS{
		fs: &awsefs.DescribeFileSystemsOutput{
			FileSystems: []efstypes.FileSystemDescription{
				{
					FileSystemId:  aws.String("fs-1"),
					FileSystemArn: aws.String("arn:aws:elasticfilesystem:us-east-1:111:file-system/fs-1"),
					Name:          aws.String("shared"),
					KmsKeyId:      aws.String("arn:aws:kms:us-east-1:111:key/efs"),
				},
			},
		},
		mts: &awsefs.DescribeMountTargetsOutput{
			MountTargets: []efstypes.MountTargetDescription{
				{MountTargetId: aws.String("fsmt-1"), SubnetId: aws.String("subnet-abc")},
			},
		},
		sgs: &awsefs.DescribeMountTargetSecurityGroupsOutput{SecurityGroups: []string{"sg-0aa"}},
	}
	rec := &recorder{region: "us-east-1"}
	edges := efsEdgesAPI(context.Background(), fake, "us-east-1", rec)
	if len(rec.errs) != 0 {
		t.Fatalf("unexpected errors: %+v", rec.errs)
	}
	want := map[string]string{
		"arn:aws:kms:us-east-1:111:key/efs": "EFS encryption key",
		"subnet-abc":                        "EFS mount target subnet",
		"sg-0aa":                            "EFS mount target security group",
	}
	if len(edges) != len(want) {
		t.Fatalf("want %d edges, got %d: %+v", len(want), len(edges), edges)
	}
	for _, e := range edges {
		if via, ok := want[e.Target]; !ok || e.From.Via != via {
			t.Errorf("edge %+v not expected (want via %q)", e, via)
		}
		if e.From.Service != "efs" {
			t.Errorf("from service = %q", e.From.Service)
		}
	}
}

func TestIsNotConfigured(t *testing.T) {
	cases := map[error]bool{
		&smithy.GenericAPIError{Code: "ReplicationConfigurationNotFoundError"}:          true,
		&smithy.GenericAPIError{Code: "ServerSideEncryptionConfigurationNotFoundError"}: true,
		&smithy.GenericAPIError{Code: "NoSuchBucketPolicy"}:                             true,
		&smithy.GenericAPIError{Code: "AccessDenied"}:                                   false,
		nil: false,
	}
	for err, want := range cases {
		if got := isNotConfigured(err); got != want {
			t.Errorf("isNotConfigured(%v) = %v, want %v", err, got, want)
		}
	}
}
