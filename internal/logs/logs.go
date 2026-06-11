// Package logs wraps the CloudWatch Logs APIs used to browse log groups and
// fetch log events. Fetching is built on FilterLogEvents — the best
// general-purpose retrieval API — but that API is throttled hard (5 TPS per
// account/region), so callers fetch one page at a time and drive pagination
// themselves instead of fanning out.
package logs

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// Group is a CloudWatch log group.
type Group struct {
	Name          string
	ARN           string
	Region        string
	RetentionDays int32 // 0 = never expire
	StoredBytes   int64
	CreatedAt     time.Time
}

// ListGroups returns every log group in the region. Best-effort: on a page
// failure the groups listed so far are returned together with the error.
func ListGroups(ctx context.Context, client cloudwatchlogs.DescribeLogGroupsAPIClient, region string) ([]Group, error) {
	var groups []Group
	paginator := cloudwatchlogs.NewDescribeLogGroupsPaginator(client, &cloudwatchlogs.DescribeLogGroupsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return groups, fmt.Errorf("failed to list log groups: %w", err)
		}
		for _, g := range page.LogGroups {
			group := Group{
				Name:          aws.ToString(g.LogGroupName),
				ARN:           aws.ToString(g.Arn),
				Region:        region,
				RetentionDays: aws.ToInt32(g.RetentionInDays),
				StoredBytes:   aws.ToInt64(g.StoredBytes),
			}
			if g.CreationTime != nil {
				group.CreatedAt = time.UnixMilli(aws.ToInt64(g.CreationTime)).UTC()
			}
			groups = append(groups, group)
		}
	}
	return groups, nil
}

// Event is a single log event.
type Event struct {
	Timestamp time.Time
	Stream    string
	Message   string
}

// FetchInput describes one FilterLogEvents page request.
type FetchInput struct {
	Group   string
	Start   time.Time
	End     time.Time // zero = now
	Pattern string    // CloudWatch Logs filter pattern; empty = all events
	Limit   int32     // max events for this page; 0 = API max (10,000)
	// NextToken continues a previous fetch; nil starts from Start.
	NextToken *string
}

// Page is one page of fetched events. A non-nil NextToken means more events
// remain in the requested window.
type Page struct {
	Events    []Event
	NextToken *string
}

// FetchEvents retrieves one page of events from a log group, oldest first,
// across all its streams.
func FetchEvents(ctx context.Context, client cloudwatchlogs.FilterLogEventsAPIClient, in FetchInput) (Page, error) {
	end := in.End
	if end.IsZero() {
		end = time.Now()
	}
	req := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(in.Group),
		StartTime:    aws.Int64(in.Start.UnixMilli()),
		EndTime:      aws.Int64(end.UnixMilli()),
		NextToken:    in.NextToken,
	}
	if in.Pattern != "" {
		req.FilterPattern = aws.String(in.Pattern)
	}
	if in.Limit > 0 {
		req.Limit = aws.Int32(in.Limit)
	}

	out, err := client.FilterLogEvents(ctx, req)
	if err != nil {
		return Page{}, fmt.Errorf("failed to fetch events from %s: %w", in.Group, err)
	}

	page := Page{NextToken: out.NextToken}
	page.Events = make([]Event, 0, len(out.Events))
	for _, e := range out.Events {
		page.Events = append(page.Events, Event{
			Timestamp: time.UnixMilli(aws.ToInt64(e.Timestamp)).UTC(),
			Stream:    aws.ToString(e.LogStreamName),
			Message:   aws.ToString(e.Message),
		})
	}
	return page, nil
}

// FormatBytes renders a byte count as a human-readable size.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// FormatRetention renders a log group's retention setting.
func FormatRetention(days int32) string {
	if days <= 0 {
		return "never expires"
	}
	return fmt.Sprintf("%dd", days)
}
