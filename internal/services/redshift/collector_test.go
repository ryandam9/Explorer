package redshift

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "redshift" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapCluster_ConstructsARN(t *testing.T) {
	res := NewCollector().mapCluster(types.Cluster{
		ClusterIdentifier: aws.String("dw"),
		ClusterStatus:     aws.String("available"),
		NodeType:          aws.String("ra3.xlplus"),
	}, "us-east-1", "123456789012")
	if res.ARN != "arn:aws:redshift:us-east-1:123456789012:cluster:dw" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.State != "available" || res.Summary["nodeType"] != "ra3.xlplus" {
		t.Errorf("unexpected mapping: %+v", res)
	}
}
