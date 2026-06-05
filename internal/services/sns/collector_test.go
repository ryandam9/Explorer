package sns

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "sns" {
		t.Errorf("Name() = %q, want %q", c.Name(), "sns")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — SNS is a regional service")
	}
}

func TestMapTopic_ExtractsNameFromARN(t *testing.T) {
	c := NewCollector()
	arn := "arn:aws:sns:us-east-1:123456789012:my-topic"
	res := c.mapTopic(aws.String(arn))

	if res.Service != "sns" {
		t.Errorf("Service = %q, want %q", res.Service, "sns")
	}
	if res.Type != "topic" {
		t.Errorf("Type = %q, want %q", res.Type, "topic")
	}
	if res.ID != arn {
		t.Errorf("ID = %q, want %q", res.ID, arn)
	}
	if res.ARN != arn {
		t.Errorf("ARN = %q, want %q", res.ARN, arn)
	}
	if res.Name != "my-topic" {
		t.Errorf("Name = %q, want %q", res.Name, "my-topic")
	}
	if res.Summary["arn"] != arn {
		t.Errorf("Summary[arn] = %q, want %q", res.Summary["arn"], arn)
	}
}

func TestMapTopic_FifoTopicName(t *testing.T) {
	c := NewCollector()
	arn := "arn:aws:sns:eu-west-1:123456789012:orders.fifo"
	res := c.mapTopic(aws.String(arn))

	if res.Name != "orders.fifo" {
		t.Errorf("Name = %q, want %q", res.Name, "orders.fifo")
	}
}

func TestMapTopic_ARNWithNoColon(t *testing.T) {
	c := NewCollector()
	// Degenerate input: no colon — name falls back to full string
	arn := "just-a-topic-name"
	res := c.mapTopic(aws.String(arn))

	if res.Name != arn {
		t.Errorf("Name = %q, want full string %q when no colon present", res.Name, arn)
	}
}

func TestMapTopic_MultipleColons(t *testing.T) {
	c := NewCollector()
	// The function picks the LAST colon, so only the final segment matters
	arn := "arn:aws:sns:us-east-1:123456789012:my-notifications"
	res := c.mapTopic(aws.String(arn))

	if res.Name != "my-notifications" {
		t.Errorf("Name = %q, want %q", res.Name, "my-notifications")
	}
}
