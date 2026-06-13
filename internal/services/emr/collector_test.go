package emr

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "emr" {
		t.Errorf("Name() = %q, want %q", c.Name(), "emr")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — EMR is a regional service")
	}
}

func TestMapCluster_BasicFields(t *testing.T) {
	c := NewCollector()
	cluster := types.ClusterSummary{
		Id:   aws.String("j-ABC123DEF456"),
		Name: aws.String("my-emr-cluster"),
		Status: &types.ClusterStatus{
			State: types.ClusterStateRunning,
		},
		NormalizedInstanceHours: aws.Int32(100),
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.Service != "emr" {
		t.Errorf("Service = %q, want %q", res.Service, "emr")
	}
	if res.Type != "cluster" {
		t.Errorf("Type = %q, want %q", res.Type, "cluster")
	}
	if res.ID != "j-ABC123DEF456" {
		t.Errorf("ID = %q, want %q", res.ID, "j-ABC123DEF456")
	}
	if res.Name != "my-emr-cluster" {
		t.Errorf("Name = %q, want %q", res.Name, "my-emr-cluster")
	}
	if res.State != "RUNNING" {
		t.Errorf("State = %q, want %q", res.State, "RUNNING")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["normalizedInstanceHours"] != "100" {
		t.Errorf("Summary[normalizedInstanceHours] = %q, want %q", res.Summary["normalizedInstanceHours"], "100")
	}
}

func TestMapCluster_WithTimeline(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 3, 1, 8, 0, 0, 0, time.UTC)
	cluster := types.ClusterSummary{
		Id:   aws.String("j-WITHTIMELINE"),
		Name: aws.String("timeline-cluster"),
		Status: &types.ClusterStatus{
			State: types.ClusterStateBootstrapping,
			Timeline: &types.ClusterTimeline{
				CreationDateTime: &created,
			},
		},
	}

	res := c.mapCluster("eu-west-1", cluster, services.DetailLevelSummary)

	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapCluster_WithoutTimeline(t *testing.T) {
	c := NewCollector()
	cluster := types.ClusterSummary{
		Id:   aws.String("j-NOTIMELINE"),
		Name: aws.String("no-timeline"),
		Status: &types.ClusterStatus{
			State: types.ClusterStateTerminated,
		},
	}

	res := c.mapCluster("us-west-2", cluster, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt when no timeline, got %v", res.CreatedAt)
	}
}

func TestMapCluster_NilStatus(t *testing.T) {
	c := NewCollector()
	// ClusterSummary.Status is a pointer; AWS can leave it nil. The mapper must
	// not panic and should fall back to an empty state.
	cluster := types.ClusterSummary{
		Id:   aws.String("j-NILSTATUS"),
		Name: aws.String("nil-status"),
		// Status is nil
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.State != "" {
		t.Errorf("State = %q, want empty when Status is nil", res.State)
	}
	if res.CreatedAt != nil {
		t.Errorf("CreatedAt = %v, want nil when Status is nil", res.CreatedAt)
	}
}

func TestMapCluster_NilNormalizedHours(t *testing.T) {
	c := NewCollector()
	cluster := types.ClusterSummary{
		Id:   aws.String("j-NILHOURS"),
		Name: aws.String("nil-hours"),
		Status: &types.ClusterStatus{
			State: types.ClusterStateWaiting,
		},
		// NormalizedInstanceHours is nil — aws.ToInt32(nil) returns 0
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.Summary["normalizedInstanceHours"] != "0" {
		t.Errorf("Summary[normalizedInstanceHours] = %q, want %q", res.Summary["normalizedInstanceHours"], "0")
	}
}
