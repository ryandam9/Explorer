// Package kinesis collects Kinesis data streams. A typed collector is needed
// because the Resource Groups Tagging API only returns tagged resources; an
// untagged stream is invisible to the broad discovery sweep.
package kinesis

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "kinesis" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := kinesis.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var token *string
	for {
		page, err := client.ListStreams(ctx, &kinesis.ListStreamsInput{NextToken: token})
		if err != nil {
			return resources, fmt.Errorf("failed to list Kinesis streams: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.StreamSummaries))
		for _, s := range page.StreamSummaries {
			batch = append(batch, c.mapStream(s, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.NextToken == nil {
			break
		}
		token = page.NextToken
	}
	return resources, nil
}

func (c *Collector) mapStream(s types.StreamSummary, region string) model.Resource {
	name := aws.ToString(s.StreamName)
	return model.Resource{
		Service:   "kinesis",
		Type:      "stream",
		Region:    region,
		ID:        name,
		Name:      name,
		ARN:       aws.ToString(s.StreamARN),
		State:     string(s.StreamStatus),
		CreatedAt: s.StreamCreationTimestamp,
	}
}
