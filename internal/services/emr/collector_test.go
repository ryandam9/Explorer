package emr

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/ryandam9/aws_explorer/internal/model"
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

func TestApplyClusterDetail_Summary(t *testing.T) {
	res := &model.Resource{Summary: map[string]string{}}
	cl := &types.Cluster{
		ReleaseLabel:        aws.String("emr-7.1.0"),
		AutoTerminate:       aws.Bool(false),
		MasterPublicDnsName: aws.String("ip-10-0-1-23.ec2.internal"),
		Applications: []types.Application{
			{Name: aws.String("Spark"), Version: aws.String("3.5.0")},
			{Name: aws.String("HBase")},
			{Name: aws.String("Oozie")},
		},
		Status: &types.ClusterStatus{
			State: types.ClusterStateTerminatedWithErrors,
			StateChangeReason: &types.ClusterStateChangeReason{
				Message: aws.String("Step failed: load-orders"),
			},
		},
	}

	applyClusterDetail(res, cl, services.DetailLevelSummary)

	if got := res.Summary["releaseLabel"]; got != "emr-7.1.0" {
		t.Errorf("releaseLabel = %q, want emr-7.1.0", got)
	}
	if got := res.Summary["applications"]; got != "Spark, HBase, Oozie" {
		t.Errorf("applications = %q, want %q", got, "Spark, HBase, Oozie")
	}
	if got := res.Summary["autoTerminate"]; got != "false" {
		t.Errorf("autoTerminate = %q, want false", got)
	}
	if got := res.Summary["stateChangeReason"]; got != "Step failed: load-orders" {
		t.Errorf("stateChangeReason = %q", got)
	}
	// Summary scope must not populate the detail blob.
	if res.Details != nil {
		t.Errorf("Details should be nil at summary scope, got %v", res.Details)
	}
}

func TestApplyClusterDetail_DetailedPopulatesDetails(t *testing.T) {
	res := &model.Resource{}
	cl := &types.Cluster{
		ReleaseLabel: aws.String("emr-6.15.0"),
		LogUri:       aws.String("s3://logs/"),
		ServiceRole:  aws.String("EMR_DefaultRole"),
		Ec2InstanceAttributes: &types.Ec2InstanceAttributes{
			Ec2SubnetId:         aws.String("subnet-abc"),
			Ec2AvailabilityZone: aws.String("us-east-1a"),
		},
	}

	applyClusterDetail(res, cl, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("Details should be populated at detailed scope")
	}
	if got := res.Details["logUri"]; got != "s3://logs/" {
		t.Errorf("Details[logUri] = %v, want s3://logs/", got)
	}
	if got := res.Details["subnetId"]; got != "subnet-abc" {
		t.Errorf("Details[subnetId] = %v, want subnet-abc", got)
	}
}

func TestMapStep_FailedCarriesReasonAndLog(t *testing.T) {
	created := time.Date(2026, 6, 14, 1, 14, 0, 0, time.UTC)
	step := types.StepSummary{
		Id:              aws.String("s-XYZ"),
		Name:            aws.String("spark-submit nightly-orders"),
		ActionOnFailure: types.ActionOnFailureTerminateCluster,
		Status: &types.StepStatus{
			State: types.StepStateFailed,
			Timeline: &types.StepTimeline{
				CreationDateTime: &created,
			},
			FailureDetails: &types.FailureDetails{
				Reason:  aws.String("Application failed"),
				LogFile: aws.String("s3://logs/j-1/steps/s-XYZ/stderr.gz"),
			},
		},
	}

	res := mapStep("us-east-1", "j-1A2B3C4D5", step)

	if res.Type != "step" {
		t.Errorf("Type = %q, want step", res.Type)
	}
	if res.State != "FAILED" {
		t.Errorf("State = %q, want FAILED", res.State)
	}
	if res.Summary["cluster"] != "j-1A2B3C4D5" {
		t.Errorf("Summary[cluster] = %q", res.Summary["cluster"])
	}
	if res.Summary["actionOnFailure"] != "TERMINATE_CLUSTER" {
		t.Errorf("Summary[actionOnFailure] = %q", res.Summary["actionOnFailure"])
	}
	if res.Summary["failureReason"] != "Application failed" {
		t.Errorf("Summary[failureReason] = %q", res.Summary["failureReason"])
	}
	if res.Summary["failureLog"] == "" {
		t.Error("Summary[failureLog] should be set on failure")
	}
}

func TestIsTerminated(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
	}{
		{"RUNNING", false},
		{"WAITING", false},
		{"TERMINATED", true},
		{"TERMINATED_WITH_ERRORS", true},
		{"terminated", true},
	} {
		if got := isTerminated(tc.state); got != tc.want {
			t.Errorf("isTerminated(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}
