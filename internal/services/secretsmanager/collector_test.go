package secretsmanager

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/user/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "secretsmanager" {
		t.Errorf("Name() = %q, want %q", c.Name(), "secretsmanager")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — Secrets Manager is a regional service")
	}
}

func TestMapSecret_BasicFields(t *testing.T) {
	c := NewCollector()
	arn := "arn:aws:secretsmanager:us-east-1:123456789012:secret:my-db-password-AbCdEf"
	secret := types.SecretListEntry{
		ARN:              aws.String(arn),
		Name:             aws.String("my-db-password"),
		KmsKeyId:         aws.String("arn:aws:kms:us-east-1:123:key/abc-def"),
		RotationEnabled:  aws.Bool(true),
		SecretVersionsToStages: map[string][]string{
			"v1": {"AWSCURRENT"},
			"v2": {"AWSPREVIOUS"},
		},
	}

	res := c.mapSecret("us-east-1", secret, services.DetailLevelSummary)

	if res.Service != "secretsmanager" {
		t.Errorf("Service = %q, want %q", res.Service, "secretsmanager")
	}
	if res.Type != "secret" {
		t.Errorf("Type = %q, want %q", res.Type, "secret")
	}
	if res.ID != arn {
		t.Errorf("ID = %q, want %q", res.ID, arn)
	}
	if res.ARN != arn {
		t.Errorf("ARN = %q, want %q", res.ARN, arn)
	}
	if res.Name != "my-db-password" {
		t.Errorf("Name = %q, want %q", res.Name, "my-db-password")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["kmsKeyId"] != "arn:aws:kms:us-east-1:123:key/abc-def" {
		t.Errorf("Summary[kmsKeyId] = %q", res.Summary["kmsKeyId"])
	}
	if res.Summary["rotationEnabled"] != "true" {
		t.Errorf("Summary[rotationEnabled] = %q, want %q", res.Summary["rotationEnabled"], "true")
	}
	if res.Summary["secretVersionsToStages"] != "2" {
		t.Errorf("Summary[secretVersionsToStages] = %q, want %q", res.Summary["secretVersionsToStages"], "2")
	}
}

func TestMapSecret_WithCreationDate(t *testing.T) {
	c := NewCollector()
	created := time.Date(2023, 12, 1, 0, 0, 0, 0, time.UTC)
	secret := types.SecretListEntry{
		ARN:         aws.String("arn:aws:secretsmanager:eu-west-1:123:secret:api-key-XyZ"),
		Name:        aws.String("api-key"),
		CreatedDate: &created,
	}

	res := c.mapSecret("eu-west-1", secret, services.DetailLevelSummary)

	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapSecret_WithLastRotatedDate(t *testing.T) {
	c := NewCollector()
	rotated := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	secret := types.SecretListEntry{
		ARN:             aws.String("arn:aws:secretsmanager:us-west-2:123:secret:rotated-AbCd"),
		Name:            aws.String("rotated"),
		RotationEnabled: aws.Bool(true),
		LastRotatedDate: &rotated,
	}

	res := c.mapSecret("us-west-2", secret, services.DetailLevelSummary)

	if !strings.Contains(res.Summary["lastRotatedDate"], "2024-01-15") {
		t.Errorf("Summary[lastRotatedDate] = %q, expected to contain 2024-01-15", res.Summary["lastRotatedDate"])
	}
}

func TestMapSecret_NoLastRotatedDate(t *testing.T) {
	c := NewCollector()
	secret := types.SecretListEntry{
		ARN:  aws.String("arn:aws:secretsmanager:us-east-1:123:secret:never-rotated-GhIj"),
		Name: aws.String("never-rotated"),
	}

	res := c.mapSecret("us-east-1", secret, services.DetailLevelSummary)

	if _, ok := res.Summary["lastRotatedDate"]; ok {
		t.Errorf("expected 'lastRotatedDate' to be absent when LastRotatedDate is nil, got %q", res.Summary["lastRotatedDate"])
	}
}

func TestMapSecret_RotationDisabled(t *testing.T) {
	c := NewCollector()
	secret := types.SecretListEntry{
		ARN:             aws.String("arn:aws:secretsmanager:us-east-1:123:secret:no-rotation-KlMn"),
		Name:            aws.String("no-rotation"),
		RotationEnabled: aws.Bool(false),
	}

	res := c.mapSecret("us-east-1", secret, services.DetailLevelSummary)

	if res.Summary["rotationEnabled"] != "false" {
		t.Errorf("Summary[rotationEnabled] = %q, want %q", res.Summary["rotationEnabled"], "false")
	}
}
