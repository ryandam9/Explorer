package cwtui

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/user/aws_explorer/internal/auth"
	"github.com/user/aws_explorer/internal/config"
)

type CWLogsClient struct {
	client *cloudwatchlogs.Client
	ctx    context.Context
}

func NewCWLogsClient(ctx context.Context, awsCfg *config.AWSConfig, region string) (*CWLogsClient, error) {
	cfg, err := auth.BuildAWSConfig(ctx, awsCfg, region)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}
	return &CWLogsClient{
		client: cloudwatchlogs.NewFromConfig(cfg),
		ctx:    ctx,
	}, nil
}

// ListLogGroups fetches up to 200 log groups from the account.
func (c *CWLogsClient) ListLogGroups(ctx context.Context, prefix string) ([]types.LogGroup, error) {
	var groups []types.LogGroup
	var nextToken *string

	for {
		input := &cloudwatchlogs.DescribeLogGroupsInput{
			NextToken: nextToken,
			Limit:     aws.Int32(50),
		}
		if prefix != "" {
			input.LogGroupNamePrefix = aws.String(prefix)
		}

		ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
		resp, err := c.client.DescribeLogGroups(ctxWithTimeout, input)
		cancel()
		if err != nil {
			return nil, err
		}

		groups = append(groups, resp.LogGroups...)
		nextToken = resp.NextToken
		if nextToken == nil || len(groups) >= 200 {
			break
		}
	}

	sort.Slice(groups, func(i, j int) bool {
		return aws.ToString(groups[i].LogGroupName) < aws.ToString(groups[j].LogGroupName)
	})

	return groups, nil
}

// ListLogStreams fetches the most active log streams for a log group.
func (c *CWLogsClient) ListLogStreams(ctx context.Context, logGroupName string, prefix string) ([]types.LogStream, error) {
	input := &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String(logGroupName),
		OrderBy:      types.OrderByLastEventTime,
		Descending:   aws.Bool(true),
		Limit:        aws.Int32(50),
	}
	if prefix != "" {
		input.LogStreamNamePrefix = aws.String(prefix)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	resp, err := c.client.DescribeLogStreams(ctxWithTimeout, input)
	cancel()
	if err != nil {
		return nil, err
	}

	return resp.LogStreams, nil
}

// GetLogEvents retrieves events from a log group/stream with options for time and search filters.
func (c *CWLogsClient) GetLogEvents(ctx context.Context, logGroupName, logStreamName, filterPattern string, limit int32) ([]types.FilteredLogEvent, error) {
	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		Limit:        aws.Int32(limit),
	}
	if logStreamName != "" {
		input.LogStreamNames = []string{logStreamName}
	}
	if filterPattern != "" {
		input.FilterPattern = aws.String(filterPattern)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	resp, err := c.client.FilterLogEvents(ctxWithTimeout, input)
	cancel()
	if err != nil {
		return nil, err
	}

	return resp.Events, nil
}
