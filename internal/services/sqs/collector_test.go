package sqs

import (
	"testing"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "sqs" {
		t.Errorf("Name() = %q, want %q", c.Name(), "sqs")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — SQS is a regional service")
	}
}

func TestMapQueue_ExtractsNameFromURL(t *testing.T) {
	c := NewCollector()
	url := "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue"
	res := c.mapQueue("us-east-1", url)

	if res.Service != "sqs" {
		t.Errorf("Service = %q, want %q", res.Service, "sqs")
	}
	if res.Type != "queue" {
		t.Errorf("Type = %q, want %q", res.Type, "queue")
	}
	if res.ID != url {
		t.Errorf("ID = %q, want %q", res.ID, url)
	}
	if res.Name != "my-queue" {
		t.Errorf("Name = %q, want %q", res.Name, "my-queue")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["url"] != url {
		t.Errorf("Summary[url] = %q, want %q", res.Summary["url"], url)
	}
}

func TestMapQueue_FifoQueueName(t *testing.T) {
	c := NewCollector()
	url := "https://sqs.eu-west-1.amazonaws.com/123456789012/orders.fifo"
	res := c.mapQueue("eu-west-1", url)

	if res.Name != "orders.fifo" {
		t.Errorf("Name = %q, want %q", res.Name, "orders.fifo")
	}
}

func TestMapQueue_URLWithNoSlash(t *testing.T) {
	c := NewCollector()
	// Degenerate case: no slash — name falls back to full URL
	url := "just-a-queue-name"
	res := c.mapQueue("us-east-1", url)

	if res.Name != url {
		t.Errorf("Name = %q, want full string %q when no slash present", res.Name, url)
	}
}

func TestMapQueue_PreservesRegion(t *testing.T) {
	c := NewCollector()
	url := "https://sqs.ap-southeast-1.amazonaws.com/123456789012/ap-queue"
	res := c.mapQueue("ap-southeast-1", url)

	if res.Region != "ap-southeast-1" {
		t.Errorf("Region = %q, want %q", res.Region, "ap-southeast-1")
	}
}
