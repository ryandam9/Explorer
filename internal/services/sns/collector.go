package sns

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
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
			return resources, fmt.Errorf("failed to list SNS topics: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Topics))
		for _, topic := range page.Topics {
			res := c.mapTopic(topic.TopicArn)
			res.Region = input.Region // SNS topics are regional
			batch = append(batch, res)
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapTopic(topicArn *string) model.Resource {
	arn := aws.ToString(topicArn)
	// Extract topic name from ARN (last segment after :)
	name := arn
	if i := strings.LastIndexByte(arn, ':'); i >= 0 {
		name = arn[i+1:]
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
