package sqs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
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
			return nil, fmt.Errorf("failed to list SQS queues: %w", err)
		}

		for _, queueURL := range page.QueueUrls {
			resources = append(resources, c.mapQueue(input.Region, queueURL))
		}
	}

	return resources, nil
}

func (c *Collector) mapQueue(region string, queueURL string) model.Resource {
	name := queueURL
	// Extract queue name from URL (last segment)
	for i := len(queueURL) - 1; i >= 0; i-- {
		if queueURL[i] == '/' {
			name = queueURL[i+1:]
			break
		}
	}

	res := model.Resource{
		Service: "sqs",
		Type:    "queue",
		Region:  region,
		ID:      queueURL,
		Name:    name,
		Summary: map[string]string{
			"url": queueURL,
		},
	}

	return res
}
