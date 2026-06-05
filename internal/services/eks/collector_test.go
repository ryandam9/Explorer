package eks

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/user/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "eks" {
		t.Errorf("Name() = %q, want %q", c.Name(), "eks")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — EKS is a regional service")
	}
}

func TestMapCluster_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 2, 15, 14, 0, 0, 0, time.UTC)
	cluster := &types.Cluster{
		Arn:      aws.String("arn:aws:eks:us-east-1:123456789012:cluster/my-k8s"),
		Name:     aws.String("my-k8s"),
		Status:   types.ClusterStatusActive,
		Version:  aws.String("1.29"),
		Endpoint: aws.String("https://ABCDEF.gr7.us-east-1.eks.amazonaws.com"),
		CreatedAt: &created,
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.Service != "eks" {
		t.Errorf("Service = %q, want %q", res.Service, "eks")
	}
	if res.Type != "cluster" {
		t.Errorf("Type = %q, want %q", res.Type, "cluster")
	}
	if res.ID != "arn:aws:eks:us-east-1:123456789012:cluster/my-k8s" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.ARN != "arn:aws:eks:us-east-1:123456789012:cluster/my-k8s" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Name != "my-k8s" {
		t.Errorf("Name = %q, want %q", res.Name, "my-k8s")
	}
	if res.State != "ACTIVE" {
		t.Errorf("State = %q, want %q", res.State, "ACTIVE")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["version"] != "1.29" {
		t.Errorf("Summary[version] = %q, want %q", res.Summary["version"], "1.29")
	}
	if res.Summary["endpoint"] != "https://ABCDEF.gr7.us-east-1.eks.amazonaws.com" {
		t.Errorf("Summary[endpoint] = %q", res.Summary["endpoint"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapCluster_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	cluster := &types.Cluster{
		Arn:    aws.String("arn:aws:eks:us-west-2:123:cluster/k"),
		Name:   aws.String("k"),
		Status: types.ClusterStatusActive,
	}

	res := c.mapCluster("us-west-2", cluster, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapCluster_DetailLevel(t *testing.T) {
	c := NewCollector()
	cluster := &types.Cluster{
		Arn:             aws.String("arn:aws:eks:eu-west-1:123:cluster/detail-k8s"),
		Name:            aws.String("detail-k8s"),
		Status:          types.ClusterStatusActive,
		RoleArn:         aws.String("arn:aws:iam::123:role/eks-role"),
		PlatformVersion: aws.String("eks.5"),
	}

	res := c.mapCluster("eu-west-1", cluster, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["roleArn"] != "arn:aws:iam::123:role/eks-role" {
		t.Errorf("Details[roleArn] = %v", res.Details["roleArn"])
	}
	if res.Details["platformVersion"] != "eks.5" {
		t.Errorf("Details[platformVersion] = %v", res.Details["platformVersion"])
	}
}

func TestMapCluster_NilCreatedAt(t *testing.T) {
	c := NewCollector()
	cluster := &types.Cluster{
		Arn:    aws.String("arn:aws:eks:us-east-1:123:cluster/no-time"),
		Name:   aws.String("no-time"),
		Status: types.ClusterStatusCreating,
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt, got %v", res.CreatedAt)
	}
}
