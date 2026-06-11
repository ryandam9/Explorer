package cwtui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// LogGroup is a CloudWatch log group annotated with the region it lives in,
// so stream/event queries can be routed to the right regional client.
type LogGroup struct {
	types.LogGroup
	Region string
}

// CWLogsClient holds one CloudWatch Logs client per region.
type CWLogsClient struct {
	clients map[string]*cloudwatchlogs.Client
	regions []string
}

// NewCWLogsClient builds per-region CloudWatch Logs clients. When allRegions
// is true the region list is discovered via ec2:DescribeRegions, falling back
// to the built-in region list when that call is denied or fails.
func NewCWLogsClient(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool) (*CWLogsClient, error) {
	bootstrap := "us-east-1"
	if len(regions) > 0 {
		bootstrap = regions[0]
	}
	base, err := auth.BuildAWSConfig(ctx, awsCfg, bootstrap)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}

	if allRegions {
		regions = resolveRegions(ctx, base)
	}
	if len(regions) == 0 {
		regions = []string{bootstrap}
	}
	sort.Strings(regions)

	clients := make(map[string]*cloudwatchlogs.Client, len(regions))
	for _, r := range regions {
		rCfg := base.Copy()
		rCfg.Region = r
		clients[r] = cloudwatchlogs.NewFromConfig(rCfg)
	}
	return &CWLogsClient{clients: clients, regions: regions}, nil
}

// Regions returns the regions this client queries, sorted.
func (c *CWLogsClient) Regions() []string {
	return c.regions
}

func (c *CWLogsClient) clientFor(region string) *cloudwatchlogs.Client {
	if cl, ok := c.clients[region]; ok {
		return cl
	}
	// Unknown region (shouldn't happen): fall back to any client.
	for _, cl := range c.clients {
		return cl
	}
	return nil
}

// resolveRegions lists all enabled regions, falling back to the built-in list
// when ec2:DescribeRegions is denied or fails.
func resolveRegions(ctx context.Context, cfg aws.Config) []string {
	client := awsec2.NewFromConfig(cfg)
	result, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		slog.Warn("Unable to list AWS regions; falling back to the built-in region list",
			"error", err.Error(), "regions", len(awsutil.FallbackRegions))
		return awsutil.FallbackRegions
	}
	var regions []string
	for _, region := range result.Regions {
		if region.RegionName != nil {
			regions = append(regions, *region.RegionName)
		}
	}
	if len(regions) == 0 {
		return awsutil.FallbackRegions
	}
	return regions
}

// ListLogGroups fans DescribeLogGroups out across every configured region in
// parallel (up to 200 groups per region). Per-region failures are soft —
// opt-in regions commonly deny access — so an error is returned only when
// every region fails.
func (c *CWLogsClient) ListLogGroups(ctx context.Context, prefix string) ([]LogGroup, error) {
	var (
		mu       sync.Mutex
		groups   []LogGroup
		firstErr error
		failures int
		wg       sync.WaitGroup
	)

	for _, region := range c.regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			regional, err := c.listLogGroupsInRegion(ctx, region, prefix)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("DescribeLogGroups failed", "region", region, "error", err.Error())
				return
			}
			groups = append(groups, regional...)
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return nil, firstErr
	}

	sort.Slice(groups, func(i, j int) bool {
		ni, nj := aws.ToString(groups[i].LogGroupName), aws.ToString(groups[j].LogGroupName)
		if ni != nj {
			return ni < nj
		}
		return groups[i].Region < groups[j].Region
	})

	return groups, nil
}

func (c *CWLogsClient) listLogGroupsInRegion(ctx context.Context, region, prefix string) ([]LogGroup, error) {
	var groups []LogGroup
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
		resp, err := c.clientFor(region).DescribeLogGroups(ctxWithTimeout, input)
		cancel()
		if err != nil {
			return nil, err
		}

		for _, g := range resp.LogGroups {
			groups = append(groups, LogGroup{LogGroup: g, Region: region})
		}
		nextToken = resp.NextToken
		if nextToken == nil || len(groups) >= 200 {
			break
		}
	}
	return groups, nil
}

// ListLogStreams fetches the most active log streams for a log group.
func (c *CWLogsClient) ListLogStreams(ctx context.Context, region, logGroupName string, prefix string) ([]types.LogStream, error) {
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
	resp, err := c.clientFor(region).DescribeLogStreams(ctxWithTimeout, input)
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
func (c *CWLogsClient) GetLogEvents(ctx context.Context, region, logGroupName, logStreamName, filterPattern string, limit int32) ([]types.FilteredLogEvent, error) {
	start := time.Now().Add(-24 * time.Hour).UnixMilli()
	return c.GetLogEventsSince(ctx, region, logGroupName, logStreamName, filterPattern, start, limit)
}

// GetLogEventsSince pages FilterLogEvents forward from startMillis (inclusive),
// keeping at most `limit` of the most recent events. The full log viewer uses
// it for the initial backfill and to stream events newer than the last one
// seen; StartTime being inclusive means the caller must de-duplicate by event
// ID across calls.
func (c *CWLogsClient) GetLogEventsSince(ctx context.Context, region, logGroupName, logStreamName, filterPattern string, startMillis int64, limit int32) ([]types.FilteredLogEvent, error) {
	const maxPages = 20

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		StartTime:    aws.Int64(startMillis),
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
		resp, err := c.clientFor(region).FilterLogEvents(ctxWithTimeout, input)
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
