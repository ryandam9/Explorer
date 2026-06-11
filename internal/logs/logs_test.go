package logs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// fakeGroupsClient pages through canned DescribeLogGroups responses, failing
// when a page's err is set.
type fakeGroupsClient struct {
	pages []struct {
		out *cloudwatchlogs.DescribeLogGroupsOutput
		err error
	}
	call int
}

func (f *fakeGroupsClient) DescribeLogGroups(_ context.Context, _ *cloudwatchlogs.DescribeLogGroupsInput,
	_ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	p := f.pages[f.call]
	f.call++
	return p.out, p.err
}

func groupsPage(next string, names ...string) *cloudwatchlogs.DescribeLogGroupsOutput {
	out := &cloudwatchlogs.DescribeLogGroupsOutput{}
	if next != "" {
		out.NextToken = aws.String(next)
	}
	for _, n := range names {
		out.LogGroups = append(out.LogGroups, types.LogGroup{
			LogGroupName:    aws.String(n),
			Arn:             aws.String("arn:aws:logs:us-east-1:123:log-group:" + n + ":*"),
			RetentionInDays: aws.Int32(30),
			StoredBytes:     aws.Int64(2048),
			CreationTime:    aws.Int64(1700000000000),
		})
	}
	return out
}

func TestListGroups_MapsFields(t *testing.T) {
	client := &fakeGroupsClient{}
	client.pages = append(client.pages, struct {
		out *cloudwatchlogs.DescribeLogGroupsOutput
		err error
	}{groupsPage("", "/aws/lambda/fn"), nil})

	groups, err := ListGroups(context.Background(), client, "us-east-1")
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	g := groups[0]
	if g.Name != "/aws/lambda/fn" || g.Region != "us-east-1" {
		t.Errorf("unexpected group: %+v", g)
	}
	if g.RetentionDays != 30 || g.StoredBytes != 2048 {
		t.Errorf("unexpected retention/bytes: %+v", g)
	}
	if g.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set from CreationTime")
	}
	if !strings.HasPrefix(g.ARN, "arn:aws:logs:") {
		t.Errorf("unexpected ARN: %q", g.ARN)
	}
}

func TestListGroups_KeepsPartialResultsOnPageFailure(t *testing.T) {
	client := &fakeGroupsClient{}
	client.pages = append(client.pages,
		struct {
			out *cloudwatchlogs.DescribeLogGroupsOutput
			err error
		}{groupsPage("next", "g1", "g2"), nil},
		struct {
			out *cloudwatchlogs.DescribeLogGroupsOutput
			err error
		}{nil, errors.New("throttled")},
	)

	groups, err := ListGroups(context.Background(), client, "us-east-1")
	if err == nil {
		t.Fatal("expected the second page failure to be reported")
	}
	if len(groups) != 2 {
		t.Fatalf("expected the 2 groups from page 1 to be kept, got %d", len(groups))
	}
}

// fakeEventsClient records the FilterLogEvents request and returns a canned
// response.
type fakeEventsClient struct {
	in  *cloudwatchlogs.FilterLogEventsInput
	out *cloudwatchlogs.FilterLogEventsOutput
	err error
}

func (f *fakeEventsClient) FilterLogEvents(_ context.Context, in *cloudwatchlogs.FilterLogEventsInput,
	_ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestFetchEvents_MapsEventsAndToken(t *testing.T) {
	client := &fakeEventsClient{
		out: &cloudwatchlogs.FilterLogEventsOutput{
			Events: []types.FilteredLogEvent{
				{
					Timestamp:     aws.Int64(1700000000000),
					LogStreamName: aws.String("stream-1"),
					Message:       aws.String("hello world"),
				},
			},
			NextToken: aws.String("more"),
		},
	}

	page, err := FetchEvents(context.Background(), client, FetchInput{
		Group: "/aws/lambda/fn",
		Start: time.Now().Add(-15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("FetchEvents: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(page.Events))
	}
	e := page.Events[0]
	if e.Stream != "stream-1" || e.Message != "hello world" {
		t.Errorf("unexpected event: %+v", e)
	}
	if e.Timestamp.UnixMilli() != 1700000000000 {
		t.Errorf("Timestamp = %v, want 1700000000000ms", e.Timestamp)
	}
	if page.NextToken == nil || *page.NextToken != "more" {
		t.Errorf("NextToken = %v, want \"more\"", page.NextToken)
	}
}

func TestFetchEvents_RequestShape(t *testing.T) {
	client := &fakeEventsClient{out: &cloudwatchlogs.FilterLogEventsOutput{}}
	start := time.Now().Add(-time.Hour)
	token := "resume"

	_, err := FetchEvents(context.Background(), client, FetchInput{
		Group:     "grp",
		Start:     start,
		Pattern:   "ERROR",
		Limit:     500,
		NextToken: &token,
	})
	if err != nil {
		t.Fatalf("FetchEvents: %v", err)
	}

	in := client.in
	if aws.ToString(in.LogGroupName) != "grp" {
		t.Errorf("LogGroupName = %q", aws.ToString(in.LogGroupName))
	}
	if aws.ToInt64(in.StartTime) != start.UnixMilli() {
		t.Errorf("StartTime = %d, want %d", aws.ToInt64(in.StartTime), start.UnixMilli())
	}
	if in.EndTime == nil || aws.ToInt64(in.EndTime) < start.UnixMilli() {
		t.Errorf("EndTime not defaulted to now: %v", in.EndTime)
	}
	if aws.ToString(in.FilterPattern) != "ERROR" {
		t.Errorf("FilterPattern = %q", aws.ToString(in.FilterPattern))
	}
	if aws.ToInt32(in.Limit) != 500 {
		t.Errorf("Limit = %d", aws.ToInt32(in.Limit))
	}
	if aws.ToString(in.NextToken) != "resume" {
		t.Errorf("NextToken = %q", aws.ToString(in.NextToken))
	}
}

func TestFetchEvents_NoPatternNoLimitOmitted(t *testing.T) {
	client := &fakeEventsClient{out: &cloudwatchlogs.FilterLogEventsOutput{}}
	_, err := FetchEvents(context.Background(), client, FetchInput{
		Group: "grp",
		Start: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("FetchEvents: %v", err)
	}
	if client.in.FilterPattern != nil {
		t.Error("expected FilterPattern to be omitted when empty")
	}
	if client.in.Limit != nil {
		t.Error("expected Limit to be omitted when zero")
	}
}

func TestFetchEvents_Error(t *testing.T) {
	client := &fakeEventsClient{err: errors.New("denied")}
	_, err := FetchEvents(context.Background(), client, FetchInput{Group: "grp", Start: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "grp") {
		t.Fatalf("expected error naming the group, got %v", err)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		2048:       "2.0 KB",
		5 << 20:    "5.0 MB",
		3 << 30:    "3.0 GB",
		1536 << 30: "1.5 TB",
	}
	for in, want := range cases {
		if got := FormatBytes(in); got != want {
			t.Errorf("FormatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatRetention(t *testing.T) {
	if got := FormatRetention(0); got != "never expires" {
		t.Errorf("FormatRetention(0) = %q", got)
	}
	if got := FormatRetention(30); got != "30d" {
		t.Errorf("FormatRetention(30) = %q", got)
	}
}
