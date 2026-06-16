package emrtui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

func TestClassifyState(t *testing.T) {
	cases := map[string]stateClass{
		"COMPLETED":              stateSuccess,
		"WAITING":                stateSuccess,
		"RUNNING":                stateRunning,
		"BOOTSTRAPPING":          stateRunning,
		"PENDING":                stateRunning,
		"FAILED":                 stateFailure,
		"TERMINATED_WITH_ERRORS": stateFailure,
		"CANCELLED":              stateFailure,
		"TERMINATED":             stateNeutral,
		"weird":                  stateNeutral,
		"running":                stateRunning, // case-insensitive
	}
	for state, want := range cases {
		if got := classifyState(state); got != want {
			t.Errorf("classifyState(%q) = %d, want %d", state, got, want)
		}
	}
}

func TestStateLabel(t *testing.T) {
	if got := stateLabel(""); got != "—" {
		t.Errorf("empty state = %q, want em dash", got)
	}
	if got := stateLabel("FAILED"); got != "✗ FAILED" {
		t.Errorf("FAILED label = %q", got)
	}
	if got := stateLabel("COMPLETED"); got != "✓ COMPLETED" {
		t.Errorf("COMPLETED label = %q", got)
	}
}

func TestFormatDuration(t *testing.T) {
	base := time.Date(2026, 6, 15, 1, 0, 0, 0, time.UTC)
	if got := formatDuration(base, base.Add(82*time.Second)); got != "1m 22s" {
		t.Errorf("82s = %q, want 1m 22s", got)
	}
	if got := formatDuration(base, base.Add(45*time.Second)); got != "45s" {
		t.Errorf("45s = %q, want 45s", got)
	}
	if got := formatDuration(base, base.Add(3780*time.Second)); got != "1h 03m" {
		t.Errorf("3780s = %q, want 1h 03m", got)
	}
	// Still running (zero end) renders as em dash.
	if got := formatDuration(base, time.Time{}); got != "—" {
		t.Errorf("zero end = %q, want em dash", got)
	}
}

func TestApplyClusterDetail(t *testing.T) {
	c := &Cluster{}
	cl := &emrtypes.Cluster{
		ReleaseLabel:        aws.String("emr-7.1.0"),
		AutoTerminate:       aws.Bool(true),
		MasterPublicDnsName: aws.String("ip-10-0-0-1.ec2.internal"),
		LogUri:              aws.String("s3://logs/"),
		Applications: []emrtypes.Application{
			{Name: aws.String("Spark")},
			{Name: aws.String("HBase")},
		},
		Status: &emrtypes.ClusterStatus{
			StateChangeReason: &emrtypes.ClusterStateChangeReason{
				Message: aws.String("User request"),
			},
		},
		Ec2InstanceAttributes: &emrtypes.Ec2InstanceAttributes{
			Ec2SubnetId: aws.String("subnet-1"),
		},
	}
	applyClusterDetail(c, cl)

	if c.ReleaseLabel != "emr-7.1.0" {
		t.Errorf("ReleaseLabel = %q", c.ReleaseLabel)
	}
	if c.Applications != "Spark, HBase" {
		t.Errorf("Applications = %q, want %q", c.Applications, "Spark, HBase")
	}
	if !c.AutoTerminate {
		t.Error("AutoTerminate should be true")
	}
	if c.StateReason != "User request" {
		t.Errorf("StateReason = %q", c.StateReason)
	}
	if c.SubnetID != "subnet-1" {
		t.Errorf("SubnetID = %q", c.SubnetID)
	}
	if !c.DetailKnown {
		t.Error("applyClusterDetail should mark DetailKnown")
	}
}

func TestStepFromSummary(t *testing.T) {
	created := time.Date(2026, 6, 14, 1, 14, 0, 0, time.UTC)
	s := emrtypes.StepSummary{
		Id:              aws.String("s-1"),
		Name:            aws.String("load"),
		ActionOnFailure: emrtypes.ActionOnFailureContinue,
		Config:          &emrtypes.HadoopStepConfig{Jar: aws.String("command-runner.jar")},
		Status: &emrtypes.StepStatus{
			State:    emrtypes.StepStateCompleted,
			Timeline: &emrtypes.StepTimeline{CreationDateTime: &created},
		},
	}
	step := stepFromSummary(s)
	if step.State != "COMPLETED" {
		t.Errorf("State = %q", step.State)
	}
	if step.ActionOnFailure != "CONTINUE" {
		t.Errorf("ActionOnFailure = %q", step.ActionOnFailure)
	}
	if step.Jar != "command-runner.jar" {
		t.Errorf("Jar = %q", step.Jar)
	}
	if !step.Created.Equal(created) {
		t.Errorf("Created = %v", step.Created)
	}
}

func TestRenderClusters_JSON(t *testing.T) {
	clusters := []Cluster{{
		ID: "j-1", Name: "prod", Region: "us-east-1", State: "WAITING",
		ReleaseLabel: "emr-7.1.0", Applications: "Spark", InstanceHours: 42,
	}}
	var buf bytes.Buffer
	if err := RenderClusters(&buf, clusters, "json", false); err != nil {
		t.Fatal(err)
	}
	var got []clusterJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 1 || got[0].ID != "j-1" || got[0].InstanceHours != 42 {
		t.Errorf("unexpected JSON: %+v", got)
	}
}

func TestRenderSteps_Table(t *testing.T) {
	steps := []Step{{
		Name: "load", State: "FAILED", ActionOnFailure: "TERMINATE_CLUSTER",
		FailureReason: "boom",
	}}
	var buf bytes.Buffer
	if err := RenderSteps(&buf, steps, "table", false); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "boom") {
		t.Errorf("table output missing fields:\n%s", out)
	}
}

func TestFilterStepsByStatus(t *testing.T) {
	steps := []Step{{State: "COMPLETED"}, {State: "FAILED"}, {State: "COMPLETED"}}
	if got := FilterStepsByStatus(steps, ""); len(got) != 3 {
		t.Errorf("empty filter = %d, want 3", len(got))
	}
	if got := FilterStepsByStatus(steps, "failed"); len(got) != 1 {
		t.Errorf("FAILED filter = %d, want 1", len(got))
	}
}

func TestFilterClustersByState(t *testing.T) {
	clusters := []Cluster{{State: "RUNNING"}, {State: "WAITING"}, {State: "TERMINATED"}}
	if got := FilterClustersByState(clusters, ""); len(got) != 3 {
		t.Errorf("empty = %d, want 3", len(got))
	}
	if got := FilterClustersByState(clusters, "RUNNING,WAITING"); len(got) != 2 {
		t.Errorf("RUNNING,WAITING = %d, want 2", len(got))
	}
	if got := FilterClustersByState(clusters, "terminated"); len(got) != 1 {
		t.Errorf("terminated = %d, want 1", len(got))
	}
}

func TestInventorySort(t *testing.T) {
	inv := Inventory{Clusters: []Cluster{
		{Name: "zeta"}, {Name: "alpha"}, {Name: "alpha", Region: "eu-west-1"},
	}}
	inv.sort()
	if inv.Clusters[0].Name != "alpha" || inv.Clusters[2].Name != "zeta" {
		t.Errorf("unexpected sort order: %+v", inv.Clusters)
	}
}
