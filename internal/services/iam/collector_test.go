package iam

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "iam" {
		t.Errorf("Name() = %q, want %q", c.Name(), "iam")
	}
	if !c.IsGlobal() {
		t.Error("IsGlobal() = false, want true — IAM is a global service")
	}
}

func TestMapRole_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	role := types.Role{
		RoleId:     aws.String("AROA123456789"),
		RoleName:   aws.String("my-role"),
		Arn:        aws.String("arn:aws:iam::123456789012:role/my-role"),
		Path:       aws.String("/"),
		CreateDate: &created,
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.Service != "iam" {
		t.Errorf("Service = %q, want %q", res.Service, "iam")
	}
	if res.Type != "role" {
		t.Errorf("Type = %q, want %q", res.Type, "role")
	}
	if res.Region != "global" {
		t.Errorf("Region = %q, want %q", res.Region, "global")
	}
	if res.ID != "AROA123456789" {
		t.Errorf("ID = %q, want %q", res.ID, "AROA123456789")
	}
	if res.Name != "my-role" {
		t.Errorf("Name = %q, want %q", res.Name, "my-role")
	}
	if res.ARN != "arn:aws:iam::123456789012:role/my-role" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["path"] != "/" {
		t.Errorf("Summary[path] = %q, want %q", res.Summary["path"], "/")
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapRole_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA000"),
		RoleName: aws.String("summary-role"),
		Arn:      aws.String("arn:aws:iam::123:role/summary-role"),
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapRole_DetailLevel(t *testing.T) {
	c := NewCollector()
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	role := types.Role{
		RoleId:                   aws.String("AROA_DETAIL"),
		RoleName:                 aws.String("detail-role"),
		Arn:                      aws.String("arn:aws:iam::123:role/detail-role"),
		AssumeRolePolicyDocument: aws.String(policy),
		Description:              aws.String("a test role"),
		MaxSessionDuration:       aws.Int32(3600),
	}

	res := c.mapRole(role, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["assumeRolePolicyDocument"] != policy {
		t.Errorf("Details[assumeRolePolicyDocument] = %v", res.Details["assumeRolePolicyDocument"])
	}
	if res.Details["description"] != "a test role" {
		t.Errorf("Details[description] = %v", res.Details["description"])
	}
	if res.Details["maxSessionDuration"] != int32(3600) {
		t.Errorf("Details[maxSessionDuration] = %v", res.Details["maxSessionDuration"])
	}
}

func TestMapRole_RawLevelAlsoPopulatesDetails(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA_RAW"),
		RoleName: aws.String("raw-role"),
		Arn:      aws.String("arn:aws:iam::123:role/raw-role"),
	}

	res := c.mapRole(role, services.DetailLevelRaw)

	if res.Details == nil {
		t.Error("expected Details to be populated at raw level")
	}
}

func TestMapRole_NilCreateDate(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA_NOTIME"),
		RoleName: aws.String("no-time-role"),
		Arn:      aws.String("arn:aws:iam::123:role/no-time-role"),
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt, got %v", res.CreatedAt)
	}
}
