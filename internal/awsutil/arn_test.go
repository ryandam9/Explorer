package awsutil

import "testing"

func TestEC2ARN(t *testing.T) {
	got := EC2ARN("us-east-1", "123456789012", "instance", "i-0abc")
	want := "arn:aws:ec2:us-east-1:123456789012:instance/i-0abc"
	if got != want {
		t.Errorf("EC2ARN = %q, want %q", got, want)
	}

	// Missing account or id yields no ARN rather than a misleading partial one.
	if got := EC2ARN("us-east-1", "", "instance", "i-0abc"); got != "" {
		t.Errorf("EC2ARN with no account = %q, want empty", got)
	}
	if got := EC2ARN("us-east-1", "123", "instance", ""); got != "" {
		t.Errorf("EC2ARN with no id = %q, want empty", got)
	}
}

func TestS3BucketARN(t *testing.T) {
	if got := S3BucketARN("my-bucket"); got != "arn:aws:s3:::my-bucket" {
		t.Errorf("S3BucketARN = %q", got)
	}
	if got := S3BucketARN(""); got != "" {
		t.Errorf("S3BucketARN(\"\") = %q, want empty", got)
	}
}

func TestRoute53ZoneARN(t *testing.T) {
	want := "arn:aws:route53:::hostedzone/Z123"
	if got := Route53ZoneARN("/hostedzone/Z123"); got != want {
		t.Errorf("Route53ZoneARN(prefixed) = %q, want %q", got, want)
	}
	if got := Route53ZoneARN("Z123"); got != want {
		t.Errorf("Route53ZoneARN(bare) = %q, want %q", got, want)
	}
}

func TestSQSARNFromURL(t *testing.T) {
	url := "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue"
	want := "arn:aws:sqs:us-east-1:123456789012:my-queue"
	if got := SQSARNFromURL(url); got != want {
		t.Errorf("SQSARNFromURL = %q, want %q", got, want)
	}

	if got := SQSARNFromURL("not-a-url"); got != "" {
		t.Errorf("SQSARNFromURL(bad) = %q, want empty", got)
	}
}
