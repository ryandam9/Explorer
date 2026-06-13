package lambda

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "lambda" {
		t.Errorf("Name() = %q, want %q", c.Name(), "lambda")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — Lambda is a regional service")
	}
}

func TestMapFunction_BasicFields(t *testing.T) {
	c := NewCollector()
	arn := "arn:aws:lambda:us-east-1:123456789012:function:my-func"
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:  aws.String(arn),
		FunctionName: aws.String("my-func"),
		Runtime:      lambdatypes.RuntimeNodejs18x,
		MemorySize:   aws.Int32(512),
		Timeout:      aws.Int32(30),
	}

	res := c.mapFunction("us-east-1", fn, services.DetailLevelSummary)

	if res.Service != "lambda" {
		t.Errorf("Service = %q, want %q", res.Service, "lambda")
	}
	if res.Type != "function" {
		t.Errorf("Type = %q, want %q", res.Type, "function")
	}
	if res.ID != arn {
		t.Errorf("ID = %q, want %q", res.ID, arn)
	}
	if res.Name != "my-func" {
		t.Errorf("Name = %q, want %q", res.Name, "my-func")
	}
	if res.ARN != arn {
		t.Errorf("ARN = %q, want %q", res.ARN, arn)
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
}

func TestMapFunction_SummaryFields(t *testing.T) {
	c := NewCollector()
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:  aws.String("arn:aws:lambda:eu-west-1:123:function:worker"),
		FunctionName: aws.String("worker"),
		Runtime:      lambdatypes.RuntimePython312,
		MemorySize:   aws.Int32(256),
		Timeout:      aws.Int32(60),
	}

	res := c.mapFunction("eu-west-1", fn, services.DetailLevelSummary)

	if res.Summary["runtime"] != string(lambdatypes.RuntimePython312) {
		t.Errorf("Summary[runtime] = %q, want %q", res.Summary["runtime"], string(lambdatypes.RuntimePython312))
	}
	if res.Summary["memory"] != "256 MB" {
		t.Errorf("Summary[memory] = %q, want %q", res.Summary["memory"], "256 MB")
	}
	if res.Summary["timeout"] != "60s" {
		t.Errorf("Summary[timeout] = %q, want %q", res.Summary["timeout"], "60s")
	}
}

func TestMapFunction_WithLastModified(t *testing.T) {
	c := NewCollector()
	modified := "2024-05-01T10:30:00.000+0000"
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:  aws.String("arn:aws:lambda:us-west-2:123:function:svc"),
		FunctionName: aws.String("svc"),
		LastModified: aws.String(modified),
	}

	res := c.mapFunction("us-west-2", fn, services.DetailLevelSummary)

	if res.Summary["lastModified"] != modified {
		t.Errorf("Summary[lastModified] = %q, want %q", res.Summary["lastModified"], modified)
	}
}

func TestMapFunction_NoLastModified(t *testing.T) {
	c := NewCollector()
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:  aws.String("arn:aws:lambda:us-east-1:123:function:no-mod"),
		FunctionName: aws.String("no-mod"),
	}

	res := c.mapFunction("us-east-1", fn, services.DetailLevelSummary)

	if _, ok := res.Summary["lastModified"]; ok {
		t.Error("expected lastModified to be absent when FunctionConfiguration.LastModified is nil")
	}
}

func TestMapFunction_DetailLevel(t *testing.T) {
	c := NewCollector()
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:   aws.String("arn:aws:lambda:us-east-1:123:function:detailed"),
		FunctionName:  aws.String("detailed"),
		Handler:       aws.String("index.handler"),
		Role:          aws.String("arn:aws:iam::123:role/lambda-role"),
		CodeSize:      2048,
		Architectures: []lambdatypes.Architecture{lambdatypes.ArchitectureArm64},
	}

	// Summary level: no Details.
	if res := c.mapFunction("us-east-1", fn, services.DetailLevelSummary); res.Details != nil {
		t.Errorf("summary level should not set Details, got %v", res.Details)
	}

	// Detailed level: Details populated.
	res := c.mapFunction("us-east-1", fn, services.DetailLevelDetailed)
	if res.Details == nil {
		t.Fatal("detailed level should populate Details")
	}
	if res.Details["handler"] != "index.handler" {
		t.Errorf("Details[handler] = %v", res.Details["handler"])
	}
	if res.Details["codeSize"] != int64(2048) {
		t.Errorf("Details[codeSize] = %v, want 2048", res.Details["codeSize"])
	}
	if archs, ok := res.Details["architectures"].([]string); !ok || len(archs) != 1 || archs[0] != "arm64" {
		t.Errorf("Details[architectures] = %v", res.Details["architectures"])
	}
}

func TestMapFunction_ZeroMemoryAndTimeout(t *testing.T) {
	c := NewCollector()
	fn := lambdatypes.FunctionConfiguration{
		FunctionArn:  aws.String("arn:aws:lambda:us-east-1:123:function:zero"),
		FunctionName: aws.String("zero"),
	}

	res := c.mapFunction("us-east-1", fn, services.DetailLevelSummary)

	if !strings.HasSuffix(res.Summary["memory"], " MB") {
		t.Errorf("Summary[memory] = %q, expected to end with ' MB'", res.Summary["memory"])
	}
	if !strings.HasSuffix(res.Summary["timeout"], "s") {
		t.Errorf("Summary[timeout] = %q, expected to end with 's'", res.Summary["timeout"])
	}
}

// pagedStubClient serves one successful ListFunctions page, then fails every
// subsequent HTTP call, simulating mid-pagination throttling or an outage.
type pagedStubClient struct {
	calls int
}

func (s *pagedStubClient) Do(req *http.Request) (*http.Response, error) {
	s.calls++
	if s.calls > 1 {
		return nil, errors.New("simulated network failure")
	}
	body := `{"Functions":[{"FunctionArn":"arn:aws:lambda:us-east-1:123:function:fn1","FunctionName":"fn1"}],"NextMarker":"page2"}`
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestCollect_KeepsPartialResultsWhenPaginationFails(t *testing.T) {
	c := NewCollector()
	input := services.CollectInput{
		AWSConfig: aws.Config{
			Region:           "us-east-1",
			Credentials:      credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
			HTTPClient:       &pagedStubClient{},
			RetryMaxAttempts: 1,
		},
		Region: "us-east-1",
	}

	resources, err := c.Collect(context.Background(), input)

	if err == nil {
		t.Fatal("expected the second page failure to be reported")
	}
	if len(resources) != 1 {
		t.Fatalf("expected the resource from the first page to be kept, got %d", len(resources))
	}
	if resources[0].Name != "fn1" {
		t.Errorf("Name = %q, want fn1", resources[0].Name)
	}
}
