package summary

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func TestBuildRows_NumberingAndSort(t *testing.T) {
	resources := []model.Resource{
		{Service: "s3", Type: "bucket", Region: "global", Name: "zeta-bucket", ARN: "arn:aws:s3:::zeta-bucket"},
		{Service: "ec2", Type: "instance", Region: "us-east-1", AZ: "us-east-1a", Name: "web", ARN: "arn:aws:ec2:us-east-1:1:instance/i-1"},
		{Service: "ec2", Type: "instance", Region: "us-east-1", AZ: "us-east-1b", Name: "api", ARN: "arn:aws:ec2:us-east-1:1:instance/i-2"},
	}

	rows := BuildRows(resources)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	// Sorted by service, type, name: ec2/api, ec2/web, s3/zeta.
	if rows[0].Name != "api" || rows[0].SNO != 1 {
		t.Errorf("row0 = %+v, want api/SNO1", rows[0])
	}
	if rows[1].Name != "web" || rows[1].SNO != 2 {
		t.Errorf("row1 = %+v, want web/SNO2", rows[1])
	}
	if rows[2].Name != "zeta-bucket" || rows[2].SNO != 3 {
		t.Errorf("row2 = %+v, want zeta-bucket/SNO3", rows[2])
	}
}

func TestBuildRows_PlaceholdersAndRegionAZ(t *testing.T) {
	rows := BuildRows([]model.Resource{
		{Service: "ec2", Type: "vpc", Region: "us-west-2"}, // no name, no AZ, no ARN
		{Service: "ec2", Type: "instance", Region: "eu-west-1", AZ: "eu-west-1c", Name: "n", ARN: "arn:x"},
	})

	// instance sorts before vpc (type "instance" < "vpc").
	inst, vpc := rows[0], rows[1]

	if inst.Type != "ec2/instance" {
		t.Errorf("Type = %q, want ec2/instance", inst.Type)
	}
	if inst.RegionAZ != "eu-west-1 / eu-west-1c" {
		t.Errorf("RegionAZ = %q, want region/az", inst.RegionAZ)
	}

	if vpc.Name != "-" || vpc.ARN != "-" {
		t.Errorf("expected placeholders for vpc, got name=%q arn=%q", vpc.Name, vpc.ARN)
	}
	if vpc.RegionAZ != "us-west-2" {
		t.Errorf("RegionAZ = %q, want us-west-2 (no AZ)", vpc.RegionAZ)
	}
}

func TestDedupe_PrefersRicherEntry(t *testing.T) {
	arn := "arn:aws:ec2:us-east-1:1:instance/i-1"
	// Same ARN from the universal sweep (tags only) and the typed collector
	// (state + AZ + summary). The richer typed entry should win.
	universal := model.Resource{Service: "ec2", Type: "instance", ARN: arn, Name: "web", Tags: map[string]string{"Name": "web"}}
	typed := model.Resource{
		Service: "ec2", Type: "instance", ARN: arn, Name: "web", State: "running",
		AZ: "us-east-1a", Summary: map[string]string{"instanceType": "t3.micro"},
	}

	out := Dedupe([]model.Resource{universal, typed})
	if len(out) != 1 {
		t.Fatalf("got %d resources, want 1 after dedupe", len(out))
	}
	if out[0].State != "running" || out[0].AZ != "us-east-1a" {
		t.Errorf("dedupe kept the poorer entry: %+v", out[0])
	}

	// Order independence: typed first should give the same winner.
	out2 := Dedupe([]model.Resource{typed, universal})
	if len(out2) != 1 || out2[0].State != "running" {
		t.Errorf("dedupe not order-independent: %+v", out2)
	}
}

func TestDedupe_KeepsARNlessResources(t *testing.T) {
	out := Dedupe([]model.Resource{
		{Service: "a", ARN: ""},
		{Service: "b", ARN: ""},
		{Service: "c", ARN: "arn:x"},
	})
	if len(out) != 3 {
		t.Errorf("got %d, want 3 (ARN-less resources are never merged)", len(out))
	}
}

func TestRender_CSVAndJSON(t *testing.T) {
	rows := BuildRows([]model.Resource{
		{Service: "s3", Type: "bucket", Region: "global", Name: "b", ARN: "arn:aws:s3:::b"},
	})

	var csvBuf bytes.Buffer
	if err := Render(&csvBuf, rows, "csv"); err != nil {
		t.Fatalf("csv render: %v", err)
	}
	if !strings.Contains(csvBuf.String(), "SNO,Name,Type,ARN,Region/AZ") {
		t.Errorf("csv missing header:\n%s", csvBuf.String())
	}
	if !strings.Contains(csvBuf.String(), "1,b,s3/bucket,arn:aws:s3:::b,global") {
		t.Errorf("csv missing data row:\n%s", csvBuf.String())
	}

	var jsonBuf bytes.Buffer
	if err := Render(&jsonBuf, rows, "json"); err != nil {
		t.Fatalf("json render: %v", err)
	}
	var out []Row
	if err := json.Unmarshal(jsonBuf.Bytes(), &out); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].Name != "b" {
		t.Errorf("json round-trip = %+v", out)
	}
}
