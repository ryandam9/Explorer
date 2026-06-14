package elasticache

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "elasticache" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v, want elasticache/false", c.Name(), c.IsGlobal())
	}
}

func TestMapCluster(t *testing.T) {
	res := NewCollector().mapCluster(types.CacheCluster{
		CacheClusterId:     aws.String("redis-prod"),
		ARN:                aws.String("arn:aws:elasticache:us-east-1:1:cluster:redis-prod"),
		CacheClusterStatus: aws.String("available"),
		Engine:             aws.String("redis"),
		CacheNodeType:      aws.String("cache.t3.micro"),
	}, "us-east-1")
	if res.Service != "elasticache" || res.Type != "cacheCluster" {
		t.Fatalf("service/type = %q/%q", res.Service, res.Type)
	}
	if res.ID != "redis-prod" || res.State != "available" || res.Summary["engine"] != "redis" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.ARN != "arn:aws:elasticache:us-east-1:1:cluster:redis-prod" {
		t.Errorf("ARN = %q", res.ARN)
	}
}
