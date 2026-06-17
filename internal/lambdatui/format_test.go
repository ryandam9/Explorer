package lambdatui

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func TestParseLambdaTime(t *testing.T) {
	cases := map[string]bool{ // input → expect non-zero
		"2026-06-15T01:14:00.000+0000": true,
		"2026-06-15T01:14:00Z":         true,
		"":                             false,
		"not-a-time":                   false,
	}
	for in, wantOK := range cases {
		got := parseLambdaTime(in)
		if got.IsZero() == wantOK {
			t.Errorf("parseLambdaTime(%q) zero=%v, want non-zero=%v", in, got.IsZero(), wantOK)
		}
	}
}

func TestRuntimeLabel(t *testing.T) {
	if got := runtimeLabel("python3.12", "Zip"); got != "python3.12" {
		t.Errorf("runtimeLabel zip = %q", got)
	}
	if got := runtimeLabel("", "Image"); got != "Image" {
		t.Errorf("runtimeLabel image = %q", got)
	}
	if got := runtimeLabel("", "Zip"); got != "—" {
		t.Errorf("runtimeLabel empty = %q", got)
	}
}

func TestStateLabel(t *testing.T) {
	if got := stateLabel(""); got != "—" {
		t.Errorf("empty state = %q", got)
	}
	if got := stateGlyph("Active"); got != "✓" {
		t.Errorf("active glyph = %q", got)
	}
	if got := stateGlyph("Failed"); got != "✗" {
		t.Errorf("failed glyph = %q", got)
	}
	if got := stateGlyph("Pending"); got != "●" {
		t.Errorf("pending glyph = %q", got)
	}
	if got := stateGlyph("Disabled"); got != "○" {
		t.Errorf("disabled glyph = %q", got)
	}
}

func TestFormatMemoryTimeoutCodeSize(t *testing.T) {
	if got := formatMemory(0); got != "—" {
		t.Errorf("memory 0 = %q", got)
	}
	if got := formatMemory(512); got != "512 MB" {
		t.Errorf("memory = %q", got)
	}
	if got := formatTimeout(0); got != "—" {
		t.Errorf("timeout 0 = %q", got)
	}
	if got := formatTimeout(30); got != "30s" {
		t.Errorf("timeout = %q", got)
	}
	if got := formatCodeSize(0); got != "—" {
		t.Errorf("codesize 0 = %q", got)
	}
	if got := formatCodeSize(2 * 1024 * 1024); got != "2.0 MB" {
		t.Errorf("codesize MB = %q", got)
	}
	if got := formatCodeSize(2048); got != "2.0 KB" {
		t.Errorf("codesize KB = %q", got)
	}
	if got := formatCodeSize(512); got != "512 B" {
		t.Errorf("codesize B = %q", got)
	}
}

func TestFunctionNameFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:lambda:us-east-1:123:function:my-fn":      "my-fn",
		"arn:aws:lambda:us-east-1:123:function:my-fn:PROD": "my-fn",
		"bare-name": "bare-name",
		"arn:aws:lambda:us-east-1:123:event-source-mapping:x": "x",
	}
	for in, want := range cases {
		if got := functionNameFromARN(in); got != want {
			t.Errorf("functionNameFromARN(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventSourceLabel(t *testing.T) {
	sqs := lambdatypes.EventSourceMappingConfiguration{
		EventSourceArn: aws.String("arn:aws:sqs:us-east-1:123:orders-queue"),
	}
	if got := eventSourceLabel(sqs); got != "sqs:orders-queue" {
		t.Errorf("sqs label = %q", got)
	}
	kinesis := lambdatypes.EventSourceMappingConfiguration{
		EventSourceArn: aws.String("arn:aws:kinesis:us-east-1:123:stream/clicks"),
	}
	if got := eventSourceLabel(kinesis); got != "kinesis:clicks" {
		t.Errorf("kinesis label = %q", got)
	}
	empty := lambdatypes.EventSourceMappingConfiguration{}
	if got := eventSourceLabel(empty); got != "—" {
		t.Errorf("empty label = %q", got)
	}
}

func TestDLQLabel(t *testing.T) {
	if got := dlqLabel(""); got != "none" {
		t.Errorf("empty dlq = %q", got)
	}
	if got := dlqLabel("arn:aws:sqs:us-east-1:123:dead-letters"); got != "sqs:dead-letters" {
		t.Errorf("dlq = %q", got)
	}
}

func TestShortTime(t *testing.T) {
	if got := shortTime(time.Time{}); got != "—" {
		t.Errorf("zero time = %q", got)
	}
}
