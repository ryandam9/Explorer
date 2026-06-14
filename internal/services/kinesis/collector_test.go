package kinesis

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
)

func TestMetadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "kinesis" || c.IsGlobal() {
		t.Errorf("Name=%q Global=%v", c.Name(), c.IsGlobal())
	}
}

func TestMapStream(t *testing.T) {
	res := NewCollector().mapStream(types.StreamSummary{
		StreamName:   aws.String("events"),
		StreamARN:    aws.String("arn:aws:kinesis:us-east-1:1:stream/events"),
		StreamStatus: types.StreamStatusActive,
	}, "us-east-1")
	if res.Service != "kinesis" || res.Type != "stream" || res.Name != "events" || res.State != "ACTIVE" {
		t.Errorf("unexpected mapping: %+v", res)
	}
}
