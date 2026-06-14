package athena

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "athena" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapWorkGroup_ConstructsARN(t *testing.T) {
	res := NewCollector().mapWorkGroup(types.WorkGroupSummary{
		Name:        aws.String("primary"),
		State:       types.WorkGroupStateEnabled,
		Description: aws.String("default wg"),
	}, "us-east-1", "123456789012")
	if res.ARN != "arn:aws:athena:us-east-1:123456789012:workgroup/primary" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.State != "ENABLED" || res.Summary["description"] != "default wg" {
		t.Errorf("unexpected mapping: %+v", res)
	}
}
