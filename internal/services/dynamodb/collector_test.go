package dynamodb

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/user/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "dynamodb" {
		t.Errorf("Name() = %q, want %q", c.Name(), "dynamodb")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — DynamoDB is a regional service")
	}
}

func TestMapTable_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 1, 20, 9, 0, 0, 0, time.UTC)
	table := &types.TableDescription{
		TableId:          aws.String("abc-123"),
		TableName:        aws.String("orders"),
		TableStatus:      types.TableStatusActive,
		ItemCount:        aws.Int64(500),
		TableSizeBytes:   aws.Int64(2097152), // 2 MB
		CreationDateTime: &created,
		BillingModeSummary: &types.BillingModeSummary{
			BillingMode: types.BillingModePayPerRequest,
		},
	}

	res := c.mapTable("us-east-1", table, services.DetailLevelSummary)

	if res.Service != "dynamodb" {
		t.Errorf("Service = %q, want %q", res.Service, "dynamodb")
	}
	if res.Type != "table" {
		t.Errorf("Type = %q, want %q", res.Type, "table")
	}
	if res.ID != "abc-123" {
		t.Errorf("ID = %q, want %q", res.ID, "abc-123")
	}
	if res.Name != "orders" {
		t.Errorf("Name = %q, want %q", res.Name, "orders")
	}
	if res.State != "ACTIVE" {
		t.Errorf("State = %q, want %q", res.State, "ACTIVE")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.Summary["billingMode"] != "PAY_PER_REQUEST" {
		t.Errorf("Summary[billingMode] = %q", res.Summary["billingMode"])
	}
	if res.Summary["itemCount"] != "500" {
		t.Errorf("Summary[itemCount] = %q, want %q", res.Summary["itemCount"], "500")
	}
	if !strings.Contains(res.Summary["tableSize"], "MB") {
		t.Errorf("Summary[tableSize] = %q, expected MB unit", res.Summary["tableSize"])
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapTable_ProvisionedBillingDefault(t *testing.T) {
	c := NewCollector()
	table := &types.TableDescription{
		TableId:     aws.String("t-prov"),
		TableName:   aws.String("sessions"),
		TableStatus: types.TableStatusActive,
		// BillingModeSummary is nil — should default to "provisioned"
	}

	res := c.mapTable("us-west-2", table, services.DetailLevelSummary)

	if res.Summary["billingMode"] != "provisioned" {
		t.Errorf("Summary[billingMode] = %q, want %q", res.Summary["billingMode"], "provisioned")
	}
}

func TestMapTable_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	table := &types.TableDescription{
		TableId:     aws.String("t-sum"),
		TableName:   aws.String("items"),
		TableStatus: types.TableStatusActive,
	}

	res := c.mapTable("eu-west-1", table, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapTable_DetailLevel(t *testing.T) {
	c := NewCollector()
	table := &types.TableDescription{
		TableId:     aws.String("t-detail"),
		TableName:   aws.String("products"),
		TableStatus: types.TableStatusActive,
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
		},
		DeletionProtectionEnabled: aws.Bool(true),
	}

	res := c.mapTable("ap-southeast-1", table, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	schemas, ok := res.Details["keySchema"].([]string)
	if !ok {
		t.Fatalf("Details[keySchema] type = %T, want []string", res.Details["keySchema"])
	}
	if len(schemas) != 2 {
		t.Errorf("keySchema length = %d, want 2", len(schemas))
	}
	if !strings.Contains(schemas[0], "pk") || !strings.Contains(schemas[0], "HASH") {
		t.Errorf("keySchema[0] = %q, expected to contain 'pk' and 'HASH'", schemas[0])
	}
	if res.Details["attributeCount"] != 2 {
		t.Errorf("Details[attributeCount] = %v, want 2", res.Details["attributeCount"])
	}
	if res.Details["deletionProtection"] != true {
		t.Errorf("Details[deletionProtection] = %v, want true", res.Details["deletionProtection"])
	}
}

func TestMapTable_NilCreationDateTime(t *testing.T) {
	c := NewCollector()
	table := &types.TableDescription{
		TableId:     aws.String("t-notime"),
		TableName:   aws.String("no-time"),
		TableStatus: types.TableStatusActive,
	}

	res := c.mapTable("us-east-1", table, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt, got %v", res.CreatedAt)
	}
}

// describeStubClient answers ListTables with two tables, then succeeds the
// DescribeTable for "good" and fails it for "bad".
type describeStubClient struct{}

func (s *describeStubClient) Do(req *http.Request) (*http.Response, error) {
	payload, _ := io.ReadAll(req.Body)
	respond := func(status int, body string) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/x-amz-json-1.0"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
	switch {
	case strings.Contains(req.Header.Get("X-Amz-Target"), "ListTables"):
		return respond(200, `{"TableNames":["good","bad"]}`)
	case strings.Contains(string(payload), `"good"`):
		return respond(200, `{"Table":{"TableName":"good","TableId":"id-1","TableStatus":"ACTIVE"}}`)
	default:
		return respond(400, `{"__type":"com.amazon.coral.validate#ValidationException","message":"simulated describe failure"}`)
	}
}

func TestCollect_FailedDescribeDropsOnlyThatTable(t *testing.T) {
	c := NewCollector()
	input := services.CollectInput{
		AWSConfig: aws.Config{
			Region:           "us-east-1",
			Credentials:      credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
			HTTPClient:       &describeStubClient{},
			RetryMaxAttempts: 1,
		},
		Region: "us-east-1",
	}

	resources, err := c.Collect(context.Background(), input)

	if err == nil {
		t.Fatal("expected the failed DescribeTable to be reported")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error should name the failed table, got: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected the successfully described table to be kept, got %d", len(resources))
	}
	if resources[0].Name != "good" {
		t.Errorf("Name = %q, want good", resources[0].Name)
	}
}
