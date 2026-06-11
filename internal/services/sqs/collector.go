package sqs

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "sqs"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := sqs.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := sqs.NewListQueuesPaginator(client, &sqs.ListQueuesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list SQS queues: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.QueueUrls))
		for _, queueURL := range page.QueueUrls {
			batch = append(batch, c.mapQueue(input.Region, queueURL))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapQueue(region string, queueURL string) model.Resource {
	// Extract queue name from URL (last segment)
	name := queueURL
	if i := strings.LastIndexByte(queueURL, '/'); i >= 0 {
		name = queueURL[i+1:]
	}

	res := model.Resource{
		Service: "sqs",
		Type:    "queue",
		Region:  region,
		ID:      queueURL,
		Name:    name,
		ARN:     awsutil.SQSARNFromURL(queueURL),
		Summary: map[string]string{
			"url": queueURL,
		},
	}

	return res
}
