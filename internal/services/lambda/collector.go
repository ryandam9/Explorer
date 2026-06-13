package lambda

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// tagConcurrency bounds parallel ListTags calls so accounts with many
// functions don't serialize on per-function tag round-trips or trip throttling.
const tagConcurrency = 8

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
		// ListFunctions doesn't return tags; fetch them per function with
		// bounded concurrency (best-effort — a tag failure must not drop the
		// function itself).
		c.applyTags(ctx, client, batch)
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

// applyTags fills each function's Tags from ListTags, fetched concurrently.
// Each goroutine writes its own slice index, so no locking is needed. Errors
// are swallowed: tags are an enrichment, not a reason to fail the collection.
func (c *Collector) applyTags(ctx context.Context, client *lambda.Client, batch []model.Resource) {
	var g errgroup.Group
	g.SetLimit(tagConcurrency)
	for i := range batch {
		if batch[i].ARN == "" {
			continue
		}
		g.Go(func() error {
			out, err := client.ListTags(ctx, &lambda.ListTagsInput{Resource: aws.String(batch[i].ARN)})
			if err != nil || len(out.Tags) == 0 {
				return nil
			}
			tags := make(map[string]string, len(out.Tags))
			for k, v := range out.Tags {
				tags[k] = v
			}
			batch[i].Tags = tags
			return nil
		})
	}
	_ = g.Wait()
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
