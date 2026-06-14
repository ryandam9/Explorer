package cloudformation

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "cloudformation" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapStack(t *testing.T) {
	res := NewCollector().mapStack(types.Stack{
		StackName:   aws.String("network"),
		StackId:     aws.String("arn:aws:cloudformation:us-east-1:1:stack/network/abc"),
		StackStatus: types.StackStatusCreateComplete,
	}, "us-east-1")
	if res.Service != "cloudformation" || res.Type != "stack" || res.Name != "network" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.ARN != "arn:aws:cloudformation:us-east-1:1:stack/network/abc" || res.State != "CREATE_COMPLETE" {
		t.Errorf("unexpected arn/state: %q / %q", res.ARN, res.State)
	}
}
