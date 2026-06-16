package apigateway

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	v1types "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	v2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "apigateway" {
		t.Errorf("Name() = %q, want apigateway", c.Name())
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — API Gateway is regional")
	}
}

func TestMapREST(t *testing.T) {
	c := NewCollector()
	api := v1types.RestApi{
		Id:          aws.String("abc123"),
		Name:        aws.String("orders-api"),
		Description: aws.String("public orders API"),
	}
	res := c.mapREST(api, "us-east-1")

	if res.Service != "apigateway" || res.Type != "rest-api" {
		t.Errorf("Service/Type = %q/%q", res.Service, res.Type)
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q", res.Region)
	}
	if res.ID != "abc123" || res.Name != "orders-api" {
		t.Errorf("ID/Name = %q/%q", res.ID, res.Name)
	}
	if res.ARN != "arn:aws:apigateway:us-east-1::/restapis/abc123" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["protocol"] != "REST" {
		t.Errorf("Summary[protocol] = %q, want REST", res.Summary["protocol"])
	}
	if res.Summary["description"] != "public orders API" {
		t.Errorf("Summary[description] = %q", res.Summary["description"])
	}
}

func TestMapREST_NoNameFallsBackToID(t *testing.T) {
	res := NewCollector().mapREST(v1types.RestApi{Id: aws.String("xyz")}, "eu-west-1")
	if res.Name != "xyz" {
		t.Errorf("Name = %q, want the id as fallback", res.Name)
	}
	if _, ok := res.Summary["description"]; ok {
		t.Error("description should be absent when empty")
	}
}

func TestMapV2_HTTP(t *testing.T) {
	c := NewCollector()
	api := v2types.Api{
		ApiId:        aws.String("h1"),
		Name:         aws.String("edge"),
		ProtocolType: v2types.ProtocolTypeHttp,
		ApiEndpoint:  aws.String("https://h1.execute-api.us-east-1.amazonaws.com"),
	}
	res := c.mapV2(api, "us-east-1")

	if res.Type != "http-api" {
		t.Errorf("Type = %q, want httpApi", res.Type)
	}
	if res.ARN != "arn:aws:apigateway:us-east-1::/apis/h1" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["protocol"] != "HTTP" {
		t.Errorf("Summary[protocol] = %q, want HTTP", res.Summary["protocol"])
	}
	if res.Summary["endpoint"] == "" {
		t.Error("Summary[endpoint] should be set")
	}
}

func TestMapV2_WebSocket(t *testing.T) {
	res := NewCollector().mapV2(v2types.Api{
		ApiId:        aws.String("w1"),
		Name:         aws.String("chat"),
		ProtocolType: v2types.ProtocolTypeWebsocket,
	}, "us-east-1")
	if res.Type != "websocket-api" {
		t.Errorf("Type = %q, want websocketApi", res.Type)
	}
}
