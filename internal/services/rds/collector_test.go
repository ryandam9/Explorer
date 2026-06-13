package rds

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "rds" {
		t.Errorf("Name() = %q, want %q", c.Name(), "rds")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — RDS is a regional service")
	}
}

func TestMapCluster_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	cluster := types.DBCluster{
		DBClusterIdentifier: aws.String("aurora-prod"),
		DBClusterArn:        aws.String("arn:aws:rds:us-east-1:123:cluster:aurora-prod"),
		Status:              aws.String("available"),
		Engine:              aws.String("aurora-postgresql"),
		EngineVersion:       aws.String("15.4"),
		EngineMode:          aws.String("provisioned"),
		ClusterCreateTime:   &created,
		TagList:             []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}},
	}

	res := c.mapCluster("us-east-1", cluster, services.DetailLevelDetailed)

	if res.Type != "cluster" {
		t.Errorf("Type = %q, want cluster", res.Type)
	}
	if res.ID != "aurora-prod" || res.Name != "aurora-prod" {
		t.Errorf("ID/Name = %q/%q, want aurora-prod", res.ID, res.Name)
	}
	if res.ARN != "arn:aws:rds:us-east-1:123:cluster:aurora-prod" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.State != "available" {
		t.Errorf("State = %q, want available", res.State)
	}
	if res.Summary["engine"] != "aurora-postgresql" || res.Summary["engineMode"] != "provisioned" {
		t.Errorf("Summary engine/mode = %q/%q", res.Summary["engine"], res.Summary["engineMode"])
	}
	if res.Tags["env"] != "prod" {
		t.Errorf("Tags[env] = %q, want prod", res.Tags["env"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
	if res.Details["memberCount"] != 0 {
		t.Errorf("Details[memberCount] = %v, want 0", res.Details["memberCount"])
	}
}

func TestMapInstance_Tags(t *testing.T) {
	c := NewCollector()
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("tagged-db"),
		DBInstanceStatus:     aws.String("available"),
		TagList: []types.Tag{
			{Key: aws.String("env"), Value: aws.String("prod")},
			{Key: aws.String("team"), Value: aws.String("payments")},
		},
	}

	res := c.mapInstance("us-east-1", instance, services.DetailLevelSummary)

	if res.Tags["env"] != "prod" {
		t.Errorf("Tags[env] = %q, want %q", res.Tags["env"], "prod")
	}
	if res.Tags["team"] != "payments" {
		t.Errorf("Tags[team] = %q, want %q", res.Tags["team"], "payments")
	}
}

func TestMapInstance_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 3, 10, 8, 0, 0, 0, time.UTC)
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("my-db"),
		DBInstanceStatus:     aws.String("available"),
		DBInstanceClass:      aws.String("db.t3.medium"),
		Engine:               aws.String("postgres"),
		EngineVersion:        aws.String("15.3"),
		InstanceCreateTime:   &created,
	}

	res := c.mapInstance("us-east-1", instance, services.DetailLevelSummary)

	if res.Service != "rds" {
		t.Errorf("Service = %q, want %q", res.Service, "rds")
	}
	if res.Type != "instance" {
		t.Errorf("Type = %q, want %q", res.Type, "instance")
	}
	if res.ID != "my-db" {
		t.Errorf("ID = %q, want %q", res.ID, "my-db")
	}
	if res.Name != "my-db" {
		t.Errorf("Name = %q, want %q", res.Name, "my-db")
	}
	if res.State != "available" {
		t.Errorf("State = %q, want %q", res.State, "available")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["instanceClass"] != "db.t3.medium" {
		t.Errorf("Summary[instanceClass] = %q", res.Summary["instanceClass"])
	}
	if res.Summary["engine"] != "postgres" {
		t.Errorf("Summary[engine] = %q", res.Summary["engine"])
	}
	if res.Summary["engineVersion"] != "15.3" {
		t.Errorf("Summary[engineVersion] = %q", res.Summary["engineVersion"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapInstance_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("db-summary"),
		DBInstanceStatus:     aws.String("available"),
	}

	res := c.mapInstance("eu-west-1", instance, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapInstance_DetailLevel(t *testing.T) {
	c := NewCollector()
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("db-detail"),
		DBInstanceStatus:     aws.String("available"),
		AllocatedStorage:     aws.Int32(100),
		MultiAZ:              aws.Bool(true),
		PubliclyAccessible:   aws.Bool(false),
	}

	res := c.mapInstance("us-west-2", instance, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["allocatedStorage"] != int32(100) {
		t.Errorf("Details[allocatedStorage] = %v", res.Details["allocatedStorage"])
	}
	if res.Details["multiAZ"] != true {
		t.Errorf("Details[multiAZ] = %v, want true", res.Details["multiAZ"])
	}
	if res.Details["publiclyAccessible"] != false {
		t.Errorf("Details[publiclyAccessible] = %v, want false", res.Details["publiclyAccessible"])
	}
}

func TestMapInstance_RawLevelAlsoPopulatesDetails(t *testing.T) {
	c := NewCollector()
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("db-raw"),
		DBInstanceStatus:     aws.String("creating"),
	}

	res := c.mapInstance("ap-southeast-1", instance, services.DetailLevelRaw)

	if res.Details == nil {
		t.Error("expected Details to be populated at raw level")
	}
}

func TestMapInstance_NilCreateTime(t *testing.T) {
	c := NewCollector()
	instance := types.DBInstance{
		DBInstanceIdentifier: aws.String("db-no-time"),
		DBInstanceStatus:     aws.String("available"),
	}

	res := c.mapInstance("us-east-1", instance, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt when not set, got %v", res.CreatedAt)
	}
}
