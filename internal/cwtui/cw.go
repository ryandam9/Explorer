package cwtui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
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
// pages oldest-first, so it scans the lookback window to the end and keeps
// the last `limit` events. A narrower lookback scans less data, which is the
// main lever for query speed on busy groups.
func (c *CWLogsClient) GetLogEvents(ctx context.Context, region, logGroupName, logStreamName, filterPattern string, lookback time.Duration, limit int32) ([]types.FilteredLogEvent, error) {
	start := time.Now().Add(-lookback).UnixMilli()
	return c.GetLogEventsSince(ctx, region, logGroupName, logStreamName, filterPattern, start, limit)
}

// SplitPatterns splits a ";"-separated pattern input into individual
// CloudWatch filter patterns, trimming whitespace and dropping empties. A
// plain single pattern (no ";") passes through unchanged, and an empty input
// yields one empty pattern ("no filter"). Each pattern runs as its own
// server-side query; an event matching ANY of them is included (OR).
func SplitPatterns(s string) []string {
	if !strings.Contains(s, ";") {
		return []string{strings.TrimSpace(s)}
	}
	var out []string
	for _, p := range strings.Split(s, ";") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// mergeDedupeSort merges per-pattern result sets into one timeline: events
// matching several patterns appear once (deduplicated by event ID), ordered
// by timestamp, keeping the most recent `limit`.
func mergeDedupeSort(batches [][]types.FilteredLogEvent, limit int32) []types.FilteredLogEvent {
	seen := make(map[string]bool)
	var merged []types.FilteredLogEvent
	for _, batch := range batches {
		for _, ev := range batch {
			id := aws.ToString(ev.EventId)
			if id != "" && seen[id] {
				continue
			}
			if id != "" {
				seen[id] = true
			}
			merged = append(merged, ev)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return aws.ToInt64(merged[i].Timestamp) < aws.ToInt64(merged[j].Timestamp)
	})
	if limit > 0 && int32(len(merged)) > limit {
		merged = merged[int32(len(merged))-limit:]
	}
	return merged
}

// GetLogEventsSinceMulti runs GetLogEventsSince once per pattern (in
// parallel — pattern counts are small) and merges the results into one
// deduplicated, time-ordered timeline. Failed patterns degrade the result:
// the successful patterns' events are returned alongside a joined error so
// the caller can show partial results WITH a visible note, never silently.
func (c *CWLogsClient) GetLogEventsSinceMulti(ctx context.Context, region, logGroupName, logStreamName string, patterns []string, startMillis int64, limit int32) ([]types.FilteredLogEvent, error) {
	if len(patterns) == 0 {
		patterns = []string{""}
	}
	batches := make([][]types.FilteredLogEvent, len(patterns))
	errs := make([]error, len(patterns))
	var wg sync.WaitGroup
	for i, pattern := range patterns {
		wg.Add(1)
		go func(i int, pattern string) {
			defer wg.Done()
			events, err := c.GetLogEventsSince(ctx, region, logGroupName, logStreamName, pattern, startMillis, limit)
			batches[i] = events // write-by-index: no shared-slice race
			if err != nil {
				errs[i] = fmt.Errorf("pattern %q: %w", pattern, err)
			}
		}(i, pattern)
	}
	wg.Wait()
	return mergeDedupeSort(batches, limit), errors.Join(errs...)
}

// downloadMaxEvents caps how many events a download ("D") fetches, bounding
// memory and API round-trips on very busy groups. Hitting the cap is surfaced
// to the user via the truncated flag — a capped download must never read as
// the complete window.
const downloadMaxEvents = 50000

// DownloadLogEvents downloads every event matching ANY of the patterns
// across the whole lookback window, merged into one deduplicated timeline.
// Unlike the panel/viewer fetches, a download must be complete or explicitly
// failed — one failed pattern fails the whole download rather than writing a
// silently partial file. truncated reports that downloadMaxEvents cut the
// result short.
func (c *CWLogsClient) DownloadLogEvents(ctx context.Context, region, logGroupName, logStreamName string, patterns []string, lookback time.Duration) ([]types.FilteredLogEvent, bool, error) {
	if len(patterns) == 0 {
		patterns = []string{""}
	}
	batches := make([][]types.FilteredLogEvent, len(patterns))
	truncs := make([]bool, len(patterns))
	errs := make([]error, len(patterns))
	var wg sync.WaitGroup
	for i, pattern := range patterns {
		wg.Add(1)
		go func(i int, pattern string) {
			defer wg.Done()
			events, truncated, err := c.downloadPattern(ctx, region, logGroupName, logStreamName, pattern, lookback)
			batches[i], truncs[i] = events, truncated
			if err != nil {
				errs[i] = fmt.Errorf("pattern %q: %w", pattern, err)
			}
		}(i, pattern)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, false, err
	}

	merged := mergeDedupeSort(batches, 0)
	truncated := false
	for _, t := range truncs {
		truncated = truncated || t
	}
	if len(merged) > downloadMaxEvents {
		merged = merged[len(merged)-downloadMaxEvents:]
		truncated = true
	}
	return merged, truncated, nil
}

// downloadPattern pages FilterLogEvents for one pattern across the whole
// lookback window, returning every matching event oldest-first — unlike
// GetLogEventsSince, which keeps only the most recent `limit`. truncated
// reports that downloadMaxEvents ended the download before the window was
// exhausted.
func (c *CWLogsClient) downloadPattern(ctx context.Context, region, logGroupName, logStreamName, filterPattern string, lookback time.Duration) ([]types.FilteredLogEvent, bool, error) {
	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		StartTime:    aws.Int64(time.Now().Add(-lookback).UnixMilli()),
	}
	if logStreamName != "" {
		input.LogStreamNames = []string{logStreamName}
	}
	if filterPattern != "" {
		input.FilterPattern = aws.String(filterPattern)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var events []types.FilteredLogEvent
	for {
		resp, err := c.clientFor(region).FilterLogEvents(ctxWithTimeout, input)
		if err != nil {
			return nil, false, err
		}
		events = append(events, resp.Events...)
		if len(events) >= downloadMaxEvents {
			truncated := len(events) > downloadMaxEvents || resp.NextToken != nil
			return events[:downloadMaxEvents], truncated, nil
		}
		if resp.NextToken == nil {
			return events, false, nil
		}
		input.NextToken = resp.NextToken
	}
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

// backfillMaxPages bounds the page sweep of a full backfill. FilterLogEvents
// returns up to 10k events (or 1 MB) per page, so this is a runaway guard for
// pathological windows, not the normal stopping condition — the sweep ends
// when the window is exhausted or the caller's event ceiling is reached.
const backfillMaxPages = 500

// GetAllLogEventsSinceMulti backfills the WHOLE query window: it pages
// FilterLogEvents to the end of the window for each pattern (in parallel)
// instead of stopping at a small event count, then merges the results into one
// deduplicated, time-ordered timeline keeping the most recent maxEvents
// (maxEvents <= 0 keeps everything).
//
// truncated reports that the window held more events than were returned —
// either the ceiling trimmed the oldest or the page budget ran out — so the
// caller can surface it; a capped backfill must never read as the whole log.
// Failed patterns degrade the result like GetLogEventsSinceMulti: what
// succeeded is returned alongside a joined error.
func (c *CWLogsClient) GetAllLogEventsSinceMulti(ctx context.Context, region, logGroupName, logStreamName string, patterns []string, startMillis int64, maxEvents int32) ([]types.FilteredLogEvent, bool, error) {
	if len(patterns) == 0 {
		patterns = []string{""}
	}
	batches := make([][]types.FilteredLogEvent, len(patterns))
	truncs := make([]bool, len(patterns))
	errs := make([]error, len(patterns))
	var wg sync.WaitGroup
	for i, pattern := range patterns {
		wg.Add(1)
		go func(i int, pattern string) {
			defer wg.Done()
			events, truncated, err := c.getAllLogEventsSince(ctx, region, logGroupName, logStreamName, pattern, startMillis, maxEvents)
			batches[i], truncs[i] = events, truncated // write-by-index: no shared-slice race
			if err != nil {
				errs[i] = fmt.Errorf("pattern %q: %w", pattern, err)
			}
		}(i, pattern)
	}
	wg.Wait()

	merged := mergeDedupeSort(batches, 0)
	truncated := false
	for _, t := range truncs {
		truncated = truncated || t
	}
	if maxEvents > 0 && int32(len(merged)) > maxEvents {
		merged = merged[int32(len(merged))-maxEvents:]
		truncated = true
	}
	return merged, truncated, errors.Join(errs...)
}

// getAllLogEventsSince pages FilterLogEvents forward from startMillis
// (inclusive) for one pattern until the window is exhausted, keeping at most
// maxEvents of the MOST RECENT events (the viewer tails, so the newest end of
// the window is the one worth keeping). truncated reports that older events
// were dropped or the page budget ended the sweep early.
func (c *CWLogsClient) getAllLogEventsSince(ctx context.Context, region, logGroupName, logStreamName, filterPattern string, startMillis int64, maxEvents int32) ([]types.FilteredLogEvent, bool, error) {
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

	// A whole-window sweep can span many pages on a busy group; give it the
	// same headroom as the download path rather than the 60s streaming budget.
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	var events []types.FilteredLogEvent
	truncated := false
	for page := 0; page < backfillMaxPages; page++ {
		resp, err := c.clientFor(region).FilterLogEvents(ctxWithTimeout, input)
		if err != nil {
			return nil, false, err
		}
		events = append(events, resp.Events...)
		if maxEvents > 0 && int32(len(events)) > maxEvents {
			events = events[int32(len(events))-maxEvents:]
			truncated = true
		}
		if resp.NextToken == nil {
			return events, truncated, nil
		}
		input.NextToken = resp.NextToken
	}
	// Page budget spent with more to read: the window was not exhausted.
	return events, true, nil
}
