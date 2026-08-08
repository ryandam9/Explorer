// Package sqstui is the interactive SQS explorer: a queue browser with an
// attributes/DLQ overview, an opt-in message peek, CloudWatch metric
// sparklines, and a jump into the CloudWatch Logs TUI for a queue's Lambda
// consumers. Everything is read-only; the one operation with an observable
// side effect (peeking increments message receive counts) sits behind an
// explicit confirmation that states it.
package sqstui

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/config"
)

// Queue is one SQS queue annotated with the region it lives in, so detail and
// peek calls route to the right regional client.
type Queue struct {
	URL    string
	Name   string
	Region string
}

// QueueDetail is everything the overview panel shows for one queue. The three
// source calls are independently best-effort: each error is kept alongside its
// data so a denied call renders as "couldn't read X", never as an empty value
// pretending to be fact.
type QueueDetail struct {
	FetchedAt  time.Time
	Attrs      map[string]string // nil when AttrsErr is set
	AttrsErr   error
	Tags       map[string]string
	TagsErr    error
	Sources    []string // URLs of queues that use this queue as their DLQ
	SourcesErr error
}

// Client holds one SQS/CloudWatch/Lambda client per region.
type Client struct {
	sqs     map[string]*sqs.Client
	cw      map[string]*cloudwatch.Client
	lambda  map[string]*lambda.Client
	regions []string
}

// NewClient builds per-region clients. When allRegions is true the region list
// is discovered via ec2:DescribeRegions, falling back to the built-in list
// when that call is denied or fails.
func NewClient(ctx context.Context, awsCfg *config.AWSConfig, regions []string, allRegions bool) (*Client, error) {
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

	c := &Client{
		sqs:     make(map[string]*sqs.Client, len(regions)),
		cw:      make(map[string]*cloudwatch.Client, len(regions)),
		lambda:  make(map[string]*lambda.Client, len(regions)),
		regions: regions,
	}
	for _, r := range regions {
		rCfg := base.Copy()
		rCfg.Region = r
		c.sqs[r] = sqs.NewFromConfig(rCfg)
		c.cw[r] = cloudwatch.NewFromConfig(rCfg)
		c.lambda[r] = lambda.NewFromConfig(rCfg)
	}
	return c, nil
}

// Regions returns the regions this client queries, sorted.
func (c *Client) Regions() []string {
	return c.regions
}

func (c *Client) sqsFor(region string) *sqs.Client {
	if cl, ok := c.sqs[region]; ok {
		return cl
	}
	for _, cl := range c.sqs {
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

// ListQueues fans ListQueues out across every configured region in parallel,
// accumulating all pages. Per-region failures are soft — opt-in regions
// commonly deny access — so an error is returned only when every region fails.
func (c *Client) ListQueues(ctx context.Context, prefix string) ([]Queue, error) {
	var (
		mu       sync.Mutex
		queues   []Queue
		firstErr error
		failures int
		wg       sync.WaitGroup
	)

	for _, region := range c.regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			regional, err := c.listQueuesInRegion(ctx, region, prefix)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", region, err)
				}
				slog.Warn("ListQueues failed", "region", region, "error", err.Error())
				return
			}
			queues = append(queues, regional...)
		}(region)
	}
	wg.Wait()

	if failures == len(c.regions) && firstErr != nil {
		return nil, firstErr
	}

	sort.Slice(queues, func(i, j int) bool {
		if queues[i].Name != queues[j].Name {
			return queues[i].Name < queues[j].Name
		}
		return queues[i].Region < queues[j].Region
	})
	return queues, nil
}

func (c *Client) listQueuesInRegion(ctx context.Context, region, prefix string) ([]Queue, error) {
	input := &sqs.ListQueuesInput{MaxResults: aws.Int32(1000)}
	if prefix != "" {
		input.QueueNamePrefix = aws.String(prefix)
	}

	ctxWithTimeout, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	var queues []Queue
	p := sqs.NewListQueuesPaginator(c.sqsFor(region), input)
	for p.HasMorePages() {
		page, err := p.NextPage(ctxWithTimeout)
		if err != nil {
			return nil, err
		}
		for _, url := range page.QueueUrls {
			queues = append(queues, Queue{URL: url, Name: queueNameFromURL(url), Region: region})
		}
	}
	return queues, nil
}

// FetchDetail loads a queue's attributes, tags and dead-letter sources. The
// three calls are independently best-effort: a denied tag read must not hide
// the attributes, and vice versa.
func (c *Client) FetchDetail(ctx context.Context, region, url string) QueueDetail {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client := c.sqsFor(region)
	d := QueueDetail{FetchedAt: time.Now()}

	attrs, err := client.GetQueueAttributes(ctxWithTimeout, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(url),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		d.AttrsErr = err
		slog.Warn("GetQueueAttributes failed", "queue", url, "error", err.Error())
	} else {
		d.Attrs = attrs.Attributes
	}

	tags, err := client.ListQueueTags(ctxWithTimeout, &sqs.ListQueueTagsInput{QueueUrl: aws.String(url)})
	if err != nil {
		d.TagsErr = err
		slog.Warn("ListQueueTags failed", "queue", url, "error", err.Error())
	} else {
		d.Tags = tags.Tags
	}

	p := sqs.NewListDeadLetterSourceQueuesPaginator(client, &sqs.ListDeadLetterSourceQueuesInput{
		QueueUrl:   aws.String(url),
		MaxResults: aws.Int32(1000),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctxWithTimeout)
		if err != nil {
			d.SourcesErr = err
			slog.Warn("ListDeadLetterSourceQueues failed", "queue", url, "error", err.Error())
			break
		}
		d.Sources = append(d.Sources, page.QueueUrls...)
	}
	return d
}

// peekMaxMessages caps how many messages one peek samples. ReceiveMessage
// returns at most 10 per call from a random subset of servers, so the peek is
// a sample by construction — the cap bounds how many receive counts one peek
// increments, and the UI labels the result as a sample, never as the queue.
const peekMaxMessages = 50

// PeekMessages samples up to peekMaxMessages visible messages WITHOUT deleting
// them. VisibilityTimeout=0 returns each message to visible immediately, so
// consumers are not starved — but SQS still increments every sampled message's
// ApproximateReceiveCount, which on a queue with a redrive policy moves those
// messages closer to the DLQ threshold. Callers must gate this behind an
// explicit confirmation stating that. Because visibility is returned
// immediately, the same message can be re-delivered within one peek, so
// results are deduplicated by MessageId; a call returning no new messages ends
// the sweep.
func (c *Client) PeekMessages(ctx context.Context, region, url string) ([]types.Message, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	client := c.sqsFor(region)
	seen := make(map[string]bool)
	var msgs []types.Message
	// Up to 10 per call; a few extra rounds cover partial batches.
	for calls := 0; calls < peekMaxMessages/10+3 && len(msgs) < peekMaxMessages; calls++ {
		resp, err := client.ReceiveMessage(ctxWithTimeout, &sqs.ReceiveMessageInput{
			QueueUrl:                    aws.String(url),
			MaxNumberOfMessages:         10,
			VisibilityTimeout:           0,
			WaitTimeSeconds:             0,
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
			MessageAttributeNames:       []string{"All"},
		})
		if err != nil {
			return msgs, err
		}
		added := 0
		for _, m := range resp.Messages {
			id := aws.ToString(m.MessageId)
			if id != "" && seen[id] {
				continue
			}
			if id != "" {
				seen[id] = true
			}
			msgs = append(msgs, m)
			added++
			if len(msgs) >= peekMaxMessages {
				break
			}
		}
		if added == 0 {
			break
		}
	}
	return msgs, nil
}

// queueMetricDefs are the sparkline metrics the m key shows. Depth and age use
// Maximum — an average would understate spikes (capacity questions need peak,
// not mean); traffic counts use Sum over each period.
var queueMetricDefs = []struct {
	name  string
	label string
	stat  string
}{
	{"ApproximateNumberOfMessagesVisible", "Messages visible", "Maximum"},
	{"ApproximateAgeOfOldestMessage", "Age of oldest (s)", "Maximum"},
	{"NumberOfMessagesSent", "Sent", "Sum"},
	{"NumberOfMessagesReceived", "Received", "Sum"},
}

// MetricSeries is one queue metric over the lookback window, ready for a
// sparkline. Empty Values means CloudWatch had no datapoints — rendered as
// "no data", never as a flat zero line.
type MetricSeries struct {
	Label  string
	Values []float64
}

// FetchQueueMetrics loads all sparkline metrics for one queue in a single
// GetMetricData call (batched — never one call per metric), over a 3-hour
// window at 5-minute periods. GetMetricData is a paid API, so callers cache
// the result and floor refreshes (metricsRefreshFloor).
func (c *Client) FetchQueueMetrics(ctx context.Context, region, queueName string) ([]MetricSeries, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cwClient, ok := c.cw[region]
	if !ok {
		return nil, fmt.Errorf("no CloudWatch client for region %s", region)
	}

	now := time.Now()
	queries := make([]cwtypes.MetricDataQuery, 0, len(queueMetricDefs))
	for i, def := range queueMetricDefs {
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(fmt.Sprintf("m%d", i)),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String("AWS/SQS"),
					MetricName: aws.String(def.name),
					Dimensions: []cwtypes.Dimension{{Name: aws.String("QueueName"), Value: aws.String(queueName)}},
				},
				Period: aws.Int32(300),
				Stat:   aws.String(def.stat),
			},
		})
	}

	resp, err := cwClient.GetMetricData(ctxWithTimeout, &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(now.Add(-3 * time.Hour)),
		EndTime:           aws.Time(now),
		MetricDataQueries: queries,
		ScanBy:            cwtypes.ScanByTimestampAscending,
	})
	if err != nil {
		return nil, err
	}

	// Results come back keyed by query id, not necessarily in request order.
	byID := make(map[string][]float64, len(resp.MetricDataResults))
	for _, r := range resp.MetricDataResults {
		byID[aws.ToString(r.Id)] = r.Values
	}
	series := make([]MetricSeries, 0, len(queueMetricDefs))
	for i, def := range queueMetricDefs {
		series = append(series, MetricSeries{Label: def.label, Values: byID[fmt.Sprintf("m%d", i)]})
	}
	return series, nil
}

// Consumer is one Lambda event source mapping reading from a queue.
type Consumer struct {
	FunctionName string
	Enabled      bool
}

// ListConsumers finds the Lambda functions mapped to the queue as an event
// source. Only Lambda event source mappings are discoverable this way — other
// consumers (ECS pollers, custom apps) leave no such record, so an empty
// result means "no Lambda consumers found", not "nothing consumes this queue".
func (c *Client) ListConsumers(ctx context.Context, region, queueARN string) ([]Consumer, error) {
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	client, ok := c.lambda[region]
	if !ok {
		return nil, fmt.Errorf("no Lambda client for region %s", region)
	}

	var consumers []Consumer
	p := lambda.NewListEventSourceMappingsPaginator(client, &lambda.ListEventSourceMappingsInput{
		EventSourceArn: aws.String(queueARN),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctxWithTimeout)
		if err != nil {
			return consumers, err
		}
		for _, m := range page.EventSourceMappings {
			fnARN := aws.ToString(m.FunctionArn)
			if fnARN == "" {
				continue
			}
			name := fnARN
			if i := strings.LastIndexByte(fnARN, ':'); i >= 0 {
				name = fnARN[i+1:]
			}
			consumers = append(consumers, Consumer{
				FunctionName: name,
				Enabled:      strings.EqualFold(aws.ToString(m.State), "Enabled"),
			})
		}
	}
	return consumers, nil
}

// queueNameFromURL extracts the queue name (the URL's last path segment).
func queueNameFromURL(url string) string {
	if i := strings.LastIndexByte(url, '/'); i >= 0 {
		return url[i+1:]
	}
	return url
}
