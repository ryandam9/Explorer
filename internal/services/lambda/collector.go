package lambda

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "lambda"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := lambda.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list Lambda functions: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Functions))
		for _, fn := range page.Functions {
			batch = append(batch, c.mapFunction(input.Region, fn, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapFunction(region string, fn types.FunctionConfiguration, detail services.DetailLevel) model.Resource {
	id := aws.ToString(fn.FunctionArn)
	name := aws.ToString(fn.FunctionName)
	runtime := string(fn.Runtime)

	res := model.Resource{
		Service: "lambda",
		Type:    "function",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id,
		Summary: map[string]string{
			"runtime": runtime,
			"memory":  fmt.Sprintf("%d MB", aws.ToInt32(fn.MemorySize)),
			"timeout": fmt.Sprintf("%ds", aws.ToInt32(fn.Timeout)),
		},
	}

	if fn.LastModified != nil {
		res.Summary["lastModified"] = *fn.LastModified
	}

	return res
}
