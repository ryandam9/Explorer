// Package apigateway collects API Gateway APIs — both the REST APIs (API
// Gateway v1) and the HTTP/WebSocket APIs (API Gateway v2). A typed collector
// is needed because the Resource Groups Tagging API only returns resources that
// are tagged; an untagged API is invisible to the broad discovery sweep.
package apigateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	v1 "github.com/aws/aws-sdk-go-v2/service/apigateway"
	v1types "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	v2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	v2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "apigateway"
}

func (c *Collector) IsGlobal() bool {
	return false
}

// Collect gathers REST and HTTP/WebSocket APIs. The two are independent API
// surfaces, so a failure in one is recorded but does not stop the other —
// matching the best-effort contract (partial results plus a joined error).
func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	var resources []model.Resource

	rest, restErr := c.collectREST(ctx, input)
	resources = input.EmitOrAppend(resources, rest)

	httpws, httpErr := c.collectV2(ctx, input)
	resources = input.EmitOrAppend(resources, httpws)

	return resources, errors.Join(restErr, httpErr)
}

func (c *Collector) collectREST(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := v1.NewFromConfig(input.AWSConfig)

	var out []model.Resource
	var position *string
	for {
		page, err := client.GetRestApis(ctx, &v1.GetRestApisInput{Position: position})
		if err != nil {
			return out, fmt.Errorf("failed to list API Gateway REST APIs: %w", err)
		}
		for _, api := range page.Items {
			out = append(out, c.mapREST(api, input.Region))
		}
		if page.Position == nil {
			break
		}
		position = page.Position
	}
	return out, nil
}

func (c *Collector) collectV2(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := v2.NewFromConfig(input.AWSConfig)

	var out []model.Resource
	var token *string
	for {
		page, err := client.GetApis(ctx, &v2.GetApisInput{NextToken: token})
		if err != nil {
			return out, fmt.Errorf("failed to list API Gateway HTTP/WebSocket APIs: %w", err)
		}
		for _, api := range page.Items {
			out = append(out, c.mapV2(api, input.Region))
		}
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return out, nil
}

func (c *Collector) mapREST(api v1types.RestApi, region string) model.Resource {
	id := aws.ToString(api.Id)
	name := aws.ToString(api.Name)
	if name == "" {
		name = id
	}
	res := model.Resource{
		Service:   "apigateway",
		Type:      "restApi",
		Region:    region,
		ID:        id,
		Name:      name,
		ARN:       fmt.Sprintf("arn:aws:apigateway:%s::/restapis/%s", region, id),
		CreatedAt: api.CreatedDate,
		Summary:   map[string]string{"protocol": "REST"},
	}
	if d := aws.ToString(api.Description); d != "" {
		res.Summary["description"] = d
	}
	return res
}

func (c *Collector) mapV2(api v2types.Api, region string) model.Resource {
	id := aws.ToString(api.ApiId)
	name := aws.ToString(api.Name)
	if name == "" {
		name = id
	}
	protocol := string(api.ProtocolType)
	resType := "httpApi"
	if api.ProtocolType == v2types.ProtocolTypeWebsocket {
		resType = "websocketApi"
	}
	res := model.Resource{
		Service:   "apigateway",
		Type:      resType,
		Region:    region,
		ID:        id,
		Name:      name,
		ARN:       fmt.Sprintf("arn:aws:apigateway:%s::/apis/%s", region, id),
		CreatedAt: api.CreatedDate,
		Summary:   map[string]string{"protocol": protocol},
	}
	if e := aws.ToString(api.ApiEndpoint); e != "" {
		res.Summary["endpoint"] = e
	}
	return res
}
