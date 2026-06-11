package ecs

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "ecs" {
		t.Errorf("Name() = %q, want %q", c.Name(), "ecs")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — ECS is a regional service")
	}
}

func TestMapCluster_BasicFields(t *testing.T) {
	c := NewCollector()
	cluster := types.Cluster{
		ClusterArn:  aws.String("arn:aws:ecs:us-east-1:123456789012:cluster/my-cluster"),
		ClusterName: aws.String("my-cluster"),
		Status:      aws.String("ACTIVE"),
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.Service != "ecs" {
		t.Errorf("Service = %q, want %q", res.Service, "ecs")
	}
	if res.Type != "cluster" {
		t.Errorf("Type = %q, want %q", res.Type, "cluster")
	}
	if res.ID != "arn:aws:ecs:us-east-1:123456789012:cluster/my-cluster" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.Name != "my-cluster" {
		t.Errorf("Name = %q, want %q", res.Name, "my-cluster")
	}
	if res.State != "ACTIVE" {
		t.Errorf("State = %q, want %q", res.State, "ACTIVE")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
}

func TestMapCluster_TaskCountsInSummary(t *testing.T) {
	c := NewCollector()
	cluster := types.Cluster{
		ClusterArn:          aws.String("arn:aws:ecs:us-west-2:123:cluster/tasks-cluster"),
		ClusterName:         aws.String("tasks-cluster"),
		Status:              aws.String("ACTIVE"),
		RunningTasksCount:   5,
		PendingTasksCount:   2,
		ActiveServicesCount: 3,
	}

	res := c.mapCluster("us-west-2", cluster, services.DetailLevelSummary)

	if res.Summary["runningTasks"] != "5" {
		t.Errorf("Summary[runningTasks] = %q, want %q", res.Summary["runningTasks"], "5")
	}
	if res.Summary["pendingTasks"] != "2" {
		t.Errorf("Summary[pendingTasks] = %q, want %q", res.Summary["pendingTasks"], "2")
	}
	if res.Summary["activeServices"] != "3" {
		t.Errorf("Summary[activeServices] = %q, want %q", res.Summary["activeServices"], "3")
	}
}

func TestMapCluster_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	cluster := types.Cluster{
		ClusterArn:  aws.String("arn:aws:ecs:us-east-1:123:cluster/c"),
		ClusterName: aws.String("c"),
		Status:      aws.String("ACTIVE"),
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapCluster_DetailLevel(t *testing.T) {
	c := NewCollector()
	cluster := types.Cluster{
		ClusterArn:                        aws.String("arn:aws:ecs:eu-central-1:123:cluster/detail-cluster"),
		ClusterName:                       aws.String("detail-cluster"),
		Status:                            aws.String("ACTIVE"),
		RegisteredContainerInstancesCount: 10,
	}

	res := c.mapCluster("eu-central-1", cluster, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["registeredContainerInstances"] != int32(10) {
		t.Errorf("Details[registeredContainerInstances] = %v, want 10", res.Details["registeredContainerInstances"])
	}
}

func TestMapCluster_RawLevelAlsoPopulatesDetails(t *testing.T) {
	c := NewCollector()
	cluster := types.Cluster{
		ClusterArn:  aws.String("arn:aws:ecs:us-east-1:123:cluster/raw"),
		ClusterName: aws.String("raw"),
		Status:      aws.String("ACTIVE"),
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelRaw)

	if res.Details == nil {
		t.Error("expected Details to be populated at raw level")
	}
}
