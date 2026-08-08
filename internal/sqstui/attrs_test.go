package sqstui

import "testing"

func TestParseRedrive(t *testing.T) {
	// SQS returns maxReceiveCount as a JSON string; the console shows it as a
	// number. Both must parse.
	tests := []struct {
		name string
		raw  string
		want Redrive
		ok   bool
	}{
		{"string count", `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:123:orders-dlq","maxReceiveCount":"5"}`,
			Redrive{TargetARN: "arn:aws:sqs:us-east-1:123:orders-dlq", MaxReceiveCount: 5}, true},
		{"numeric count", `{"deadLetterTargetArn":"arn:aws:sqs:us-east-1:123:orders-dlq","maxReceiveCount":3}`,
			Redrive{TargetARN: "arn:aws:sqs:us-east-1:123:orders-dlq", MaxReceiveCount: 3}, true},
		{"empty", "", Redrive{}, false},
		{"garbage", "not json", Redrive{}, false},
		{"missing target", `{"maxReceiveCount":3}`, Redrive{}, false},
	}
	for _, tt := range tests {
		got, ok := parseRedrive(tt.raw)
		if ok != tt.ok || got != tt.want {
			t.Errorf("%s: parseRedrive(%q) = %+v, %v; want %+v, %v", tt.name, tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestAttrHelpersDistinguishUnknownFromZero(t *testing.T) {
	attrs := map[string]string{
		"ApproximateNumberOfMessages": "12",
		"VisibilityTimeout":           "30",
		"MessageRetentionPeriod":      "345600",
		"MaximumMessageSize":          "262144",
		"CreatedTimestamp":            "1700000000",
	}

	if got := attrCount(attrs, "ApproximateNumberOfMessages"); got != "~12" {
		t.Errorf("attrCount = %q, want ~12", got)
	}
	// A missing attribute is unknown — it must never render as 0.
	if got := attrCount(attrs, "ApproximateNumberOfMessagesDelayed"); got != unknownValue {
		t.Errorf("missing count = %q, want %q", got, unknownValue)
	}
	if got := attrSeconds(attrs, "VisibilityTimeout"); got != "30s" {
		t.Errorf("attrSeconds = %q, want 30s", got)
	}
	if got := attrSeconds(attrs, "MessageRetentionPeriod"); got != "4d" {
		t.Errorf("retention = %q, want 4d", got)
	}
	if got := attrSeconds(attrs, "DelaySeconds"); got != unknownValue {
		t.Errorf("missing seconds = %q, want %q", got, unknownValue)
	}
	if got := attrBytes(attrs, "MaximumMessageSize"); got != "256 KiB" {
		t.Errorf("attrBytes = %q, want 256 KiB", got)
	}
	if got := attrEpoch(attrs, "LastModifiedTimestamp"); got != unknownValue {
		t.Errorf("missing epoch = %q, want %q", got, unknownValue)
	}
}

func TestFormatSeconds(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "0s"}, {45, "45s"}, {60, "1m"}, {90, "90s"}, {3600, "1h"}, {86400, "1d"}, {345600, "4d"},
	}
	for _, tt := range tests {
		if got := formatSeconds(tt.n); got != tt.want {
			t.Errorf("formatSeconds(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestEncryptionLabel(t *testing.T) {
	if got := encryptionLabel(map[string]string{"KmsMasterKeyId": "alias/my-key"}); got != "KMS (alias/my-key)" {
		t.Errorf("kms label = %q", got)
	}
	if got := encryptionLabel(map[string]string{"SqsManagedSseEnabled": "true"}); got != "SSE-SQS" {
		t.Errorf("sse label = %q", got)
	}
	if got := encryptionLabel(map[string]string{}); got != "none" {
		t.Errorf("none label = %q", got)
	}
}

func TestQueueNameHelpers(t *testing.T) {
	if got := queueNameFromURL("https://sqs.us-east-1.amazonaws.com/123456789012/orders-queue"); got != "orders-queue" {
		t.Errorf("queueNameFromURL = %q", got)
	}
	if got := queueNameFromARN("arn:aws:sqs:us-east-1:123456789012:orders-dlq"); got != "orders-dlq" {
		t.Errorf("queueNameFromARN = %q", got)
	}
}

func TestIsFifo(t *testing.T) {
	if !isFifo(map[string]string{"FifoQueue": "true"}) {
		t.Error("FifoQueue=true should be FIFO")
	}
	if isFifo(map[string]string{}) {
		t.Error("missing FifoQueue attribute is not FIFO")
	}
}
