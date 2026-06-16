package s3

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestMapBucket_UsesBucketRegion(t *testing.T) {
	created := time.Date(2024, 3, 1, 9, 30, 0, 0, time.UTC)
	res := mapBucket(types.Bucket{
		Name:         aws.String("my-sydney-bucket"),
		BucketRegion: aws.String("ap-southeast-2"),
		CreationDate: &created,
	})

	if res.Region != "ap-southeast-2" {
		t.Errorf("Region = %q, want ap-southeast-2", res.Region)
	}
	if res.ID != "my-sydney-bucket" || res.Name != "my-sydney-bucket" {
		t.Errorf("ID/Name = %q/%q", res.ID, res.Name)
	}
	if res.Type != "bucket" || res.Service != "s3" {
		t.Errorf("Service/Type = %q/%q", res.Service, res.Type)
	}
	if res.Summary["creationDate"] != "2024-03-01 09:30:00" {
		t.Errorf("creationDate = %q", res.Summary["creationDate"])
	}
}

func TestMapBucket_MissingRegionFallsBackToGlobal(t *testing.T) {
	res := mapBucket(types.Bucket{Name: aws.String("legacy-bucket")})

	if res.Region != "global" {
		t.Errorf("Region = %q, want global fallback", res.Region)
	}
	if res.Summary["creationDate"] != "" {
		t.Errorf("expected empty creationDate, got %q", res.Summary["creationDate"])
	}
}
