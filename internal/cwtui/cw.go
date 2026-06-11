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
		Limit:        aws.Int32(50),
	}
	if prefix != "" {
		// The API rejects OrderBy=LastEventTime combined with a name prefix,
		// so prefix queries fall back to the default (name) ordering.
		input.LogStreamNamePrefix = aws.String(prefix)
	} else {
		input.OrderBy = types.OrderByLastEventTime
		input.Descending = aws.Bool(true)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	resp, err := c.client.DescribeLogStreams(ctxWithTimeout, input)
	cancel()
	if err != nil {
		return nil, err
	}

	return resp.LogStreams, nil
}

// GetLogEvents retrieves the most recent events from a log group/stream,
// optionally constrained by a server-side filter pattern. FilterLogEvents
// pages oldest-first, so it scans a 24-hour lookback window to the end and
// keeps the last `limit` events.
func (c *CWLogsClient) GetLogEvents(ctx context.Context, logGroupName, logStreamName, filterPattern string, limit int32) ([]types.FilteredLogEvent, error) {
	const maxPages = 20

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		StartTime:    aws.Int64(time.Now().Add(-24 * time.Hour).UnixMilli()),
	}
	if logStreamName != "" {
		input.LogStreamNames = []string{logStreamName}
	}
	if filterPattern != "" {
		input.FilterPattern = aws.String(filterPattern)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var events []types.FilteredLogEvent
	for page := 0; page < maxPages; page++ {
		resp, err := c.client.FilterLogEvents(ctxWithTimeout, input)
		if err != nil {
			return nil, err
		}
		events = append(events, resp.Events...)
		if int32(len(events)) > limit {
			events = events[int32(len(events))-limit:]
		}
		if resp.NextToken == nil {
			break
		}
		input.NextToken = resp.NextToken
	}
	return events, nil
}
