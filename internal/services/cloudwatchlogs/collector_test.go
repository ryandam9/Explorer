package cloudwatchlogs

import (
	"testing"
	"time"

	"github.com/user/aws_explorer/internal/logs"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "cloudwatchlogs" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cloudwatchlogs")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — log groups are regional")
	}
}

func TestMapGroup_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
	g := logs.Group{
		Name:          "/aws/lambda/my-fn",
		ARN:           "arn:aws:logs:us-east-1:123:log-group:/aws/lambda/my-fn:*",
		Region:        "us-east-1",
		RetentionDays: 14,
		StoredBytes:   3 << 20,
		CreatedAt:     created,
	}

	res := c.mapGroup(g)

	if res.Service != "cloudwatchlogs" || res.Type != "log_group" {
		t.Errorf("Service/Type = %q/%q", res.Service, res.Type)
	}
	if res.ID != g.Name || res.Name != g.Name {
		t.Errorf("ID/Name = %q/%q, want %q", res.ID, res.Name, g.Name)
	}
	if res.ARN != g.ARN || res.Region != "us-east-1" {
		t.Errorf("ARN/Region = %q/%q", res.ARN, res.Region)
	}
	if res.Summary["retention"] != "14d" {
		t.Errorf("Summary[retention] = %q, want 14d", res.Summary["retention"])
	}
	if res.Summary["storedSize"] != "3.0 MB" {
		t.Errorf("Summary[storedSize] = %q, want 3.0 MB", res.Summary["storedSize"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapGroup_NoRetentionNoCreation(t *testing.T) {
	c := NewCollector()
	res := c.mapGroup(logs.Group{Name: "g", Region: "us-east-1"})

	if res.Summary["retention"] != "never expires" {
		t.Errorf("Summary[retention] = %q, want \"never expires\"", res.Summary["retention"])
	}
	if res.CreatedAt != nil {
		t.Error("expected CreatedAt to be nil when the group has no creation time")
	}
}
