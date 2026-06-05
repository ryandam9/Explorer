package route53

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/user/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "route53" {
		t.Errorf("Name() = %q, want %q", c.Name(), "route53")
	}
	if !c.IsGlobal() {
		t.Error("IsGlobal() = false, want true — Route53 is a global service")
	}
}

func TestMapZone_BasicFields(t *testing.T) {
	c := NewCollector()
	zone := types.HostedZone{
		Id:                     aws.String("/hostedzone/Z1PA6795UKMFR9"),
		Name:                   aws.String("example.com."),
		ResourceRecordSetCount: aws.Int64(42),
		Config: &types.HostedZoneConfig{
			PrivateZone: false,
		},
	}

	res := c.mapZone(zone, services.DetailLevelSummary)

	if res.Service != "route53" {
		t.Errorf("Service = %q, want %q", res.Service, "route53")
	}
	if res.Type != "hostedZone" {
		t.Errorf("Type = %q, want %q", res.Type, "hostedZone")
	}
	if res.ID != "/hostedzone/Z1PA6795UKMFR9" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.Name != "example.com." {
		t.Errorf("Name = %q, want %q", res.Name, "example.com.")
	}
	if res.Region != "global" {
		t.Errorf("Region = %q, want %q", res.Region, "global")
	}
	if res.Summary["recordCount"] != "42" {
		t.Errorf("Summary[recordCount] = %q, want %q", res.Summary["recordCount"], "42")
	}
	if res.Summary["privateZone"] != "false" {
		t.Errorf("Summary[privateZone] = %q, want %q", res.Summary["privateZone"], "false")
	}
}

func TestMapZone_PrivateZone(t *testing.T) {
	c := NewCollector()
	zone := types.HostedZone{
		Id:                     aws.String("/hostedzone/ZPRIVATE123"),
		Name:                   aws.String("internal.example.com."),
		ResourceRecordSetCount: aws.Int64(10),
		Config: &types.HostedZoneConfig{
			PrivateZone: true,
		},
	}

	res := c.mapZone(zone, services.DetailLevelSummary)

	if res.Summary["privateZone"] != "true" {
		t.Errorf("Summary[privateZone] = %q, want %q", res.Summary["privateZone"], "true")
	}
}

func TestMapZone_WithComment(t *testing.T) {
	c := NewCollector()
	zone := types.HostedZone{
		Id:                     aws.String("/hostedzone/ZWITHCOMMENT"),
		Name:                   aws.String("commented.example.com."),
		ResourceRecordSetCount: aws.Int64(5),
		Config: &types.HostedZoneConfig{
			Comment:     aws.String("production zone"),
			PrivateZone: false,
		},
	}

	res := c.mapZone(zone, services.DetailLevelSummary)

	if res.Summary["comment"] != "production zone" {
		t.Errorf("Summary[comment] = %q, want %q", res.Summary["comment"], "production zone")
	}
}

func TestMapZone_WithoutComment(t *testing.T) {
	c := NewCollector()
	zone := types.HostedZone{
		Id:                     aws.String("/hostedzone/ZNOCOMMENT"),
		Name:                   aws.String("no-comment.example.com."),
		ResourceRecordSetCount: aws.Int64(3),
		Config: &types.HostedZoneConfig{
			PrivateZone: false,
			Comment:     nil,
		},
	}

	res := c.mapZone(zone, services.DetailLevelSummary)

	if _, ok := res.Summary["comment"]; ok {
		t.Errorf("expected 'comment' key to be absent when Comment is nil, got %q", res.Summary["comment"])
	}
}

func TestMapZone_ZeroRecordCount(t *testing.T) {
	c := NewCollector()
	zone := types.HostedZone{
		Id:                     aws.String("/hostedzone/ZEMPTY"),
		Name:                   aws.String("empty.example.com."),
		ResourceRecordSetCount: aws.Int64(0),
		Config:                 &types.HostedZoneConfig{},
	}

	res := c.mapZone(zone, services.DetailLevelSummary)

	if res.Summary["recordCount"] != "0" {
		t.Errorf("Summary[recordCount] = %q, want %q", res.Summary["recordCount"], "0")
	}
}
