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

func TestParseARN(t *testing.T) {
	cases := []struct {
		arn                                 string
		service, region, account, rtype, id string
	}{
		{
			"arn:aws:ec2:us-east-1:123456789012:instance/i-0abc",
			"ec2", "us-east-1", "123456789012", "instance", "i-0abc",
		},
		{
			"arn:aws:s3:::my-bucket",
			"s3", "", "", "", "my-bucket",
		},
		{
			"arn:aws:iam::123456789012:role/admin",
			"iam", "", "123456789012", "role", "admin",
		},
		{
			"arn:aws:sqs:us-east-1:123456789012:my-queue",
			"sqs", "us-east-1", "123456789012", "", "my-queue",
		},
		{
			"arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/my-alb/abc123",
			"elasticloadbalancing", "us-east-1", "123", "loadbalancer", "app/my-alb/abc123",
		},
		{
			// API Gateway uses a leading-slash resource; the first segment is the type.
			"arn:aws:apigateway:us-east-1::/restapis/37koc78zhe",
			"apigateway", "us-east-1", "", "restapis", "37koc78zhe",
		},
	}
	for _, c := range cases {
		got, ok := ParseARN(c.arn)
		if !ok {
			t.Errorf("ParseARN(%q) returned ok=false", c.arn)
			continue
		}
		if got.Service != c.service || got.Region != c.region || got.AccountID != c.account ||
			got.ResourceType != c.rtype || got.ResourceID != c.id {
			t.Errorf("ParseARN(%q) = %+v, want service=%q region=%q account=%q type=%q id=%q",
				c.arn, got, c.service, c.region, c.account, c.rtype, c.id)
		}
	}

	if _, ok := ParseARN("not-an-arn"); ok {
		t.Error("ParseARN(non-arn) returned ok=true")
	}
}

func TestARNName(t *testing.T) {
	a, _ := ParseARN("arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/my-alb/abc123")
	if got := a.ARNName(); got != "abc123" {
		t.Errorf("ARNName = %q, want abc123", got)
	}
	b, _ := ParseARN("arn:aws:s3:::my-bucket")
	if got := b.ARNName(); got != "my-bucket" {
		t.Errorf("ARNName = %q, want my-bucket", got)
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
