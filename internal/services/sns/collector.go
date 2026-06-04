package sns

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "sns"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := sns.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := sns.NewListTopicsPaginator(client, &sns.ListTopicsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list SNS topics: %w", err)
		}

		for _, topic := range page.Topics {
			resources = append(resources, c.mapTopic(topic.TopicArn))
		}
	}

	return resources, nil
}

func (c *Collector) mapTopic(topicArn *string) model.Resource {
	arn := *topicArn
	// Extract topic name from ARN (last segment after :)
	name := arn
	for i := len(arn) - 1; i >= 0; i-- {
		if arn[i] == ':' {
			name = arn[i+1:]
			break
		}
	}

	res := model.Resource{
		Service: "sns",
		Type:    "topic",
		ID:      arn,
		Name:    name,
		ARN:     arn,
		Summary: map[string]string{
			"arn": arn,
		},
	}

	return res
}
