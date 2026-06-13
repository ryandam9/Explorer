package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxQueueChecks = 200
	maxTopicChecks = 200

	// receiveActivityWindow is the lookback for "does anyone poll this
	// queue?" — 24h per the spec.
	receiveActivityWindow = 24 * time.Hour
)

// collectMessagingRegion gathers the SQS/SNS plumbing snapshot for one
// region. Same best-effort contract as the other collectors.
func collectMessagingRegion(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.MessagingSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.MessagingSnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	collectQueuesMessaging(ctx, cfg, &snap, rec, perCallTimeout)
	fetchQueueReceiveActivity(ctx, cfg, &snap, rec, perCallTimeout)
	collectTopicsMessaging(ctx, cfg, &snap, rec, perCallTimeout)

	return snap, rec.errs
}

func collectQueuesMessaging(ctx context.Context, cfg aws.Config, snap *findings.MessagingSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awssqs.NewFromConfig(cfg)

	var urls []string
	complete := true
	pager := awssqs.NewListQueuesPaginator(client, &awssqs.ListQueuesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("sqs", err)
			return
		}
		urls = append(urls, page.QueueUrls...)
		if len(urls) >= maxQueueChecks {
			rec.recordTruncation("sqs", "queues", maxQueueChecks)
			urls = urls[:maxQueueChecks]
			complete = false
			break
		}
	}

	attrNames := []sqstypes.QueueAttributeName{
		sqstypes.QueueAttributeNameQueueArn,
		sqstypes.QueueAttributeNameApproximateNumberOfMessages,
		sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
		sqstypes.QueueAttributeNameRedrivePolicy,
	}
	for _, u := range urls {
		out, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
			QueueUrl: aws.String(u), AttributeNames: attrNames,
		})
		if err != nil {
			rec.record("sqs", err)
			complete = false
			break
		}
		name := u
		if i := strings.LastIndexByte(u, '/'); i >= 0 {
			name = u[i+1:]
		}
		snap.Queues = append(snap.Queues, findings.MsgQueue{
			Name:       name,
			ARN:        out.Attributes[string(sqstypes.QueueAttributeNameQueueArn)],
			Depth:      atoi64(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]),
			NotVisible: atoi64(out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessagesNotVisible)]),
			RedriveARN: findings.ParseRedriveTarget(out.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]),
		})
	}
	snap.QueuesComplete = complete
}

func atoi64(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// fetchQueueReceiveActivity sums 24h of receive activity (empty receives +
// messages received) for the queues the no-consumers check cares about:
// depth > 0 with nothing in flight. Skipped queues keep
// ReceiveActivityKnown=false, so the check stays silent for them.
func fetchQueueReceiveActivity(ctx context.Context, cfg aws.Config, snap *findings.MessagingSnapshot, rec *errRecorder, timeout time.Duration) {
	var candidates []int // indices into snap.Queues
	for i, q := range snap.Queues {
		if q.Depth > 0 && q.NotVisible == 0 {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return
	}

	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awscloudwatch.NewFromConfig(cfg)

	period := int32(receiveActivityWindow / time.Second)
	var queries []cwtypes.MetricDataQuery
	bind := map[string]int{} // query ID → queue index
	for n, qi := range candidates {
		for m, metric := range []string{"NumberOfEmptyReceives", "NumberOfMessagesReceived"} {
			id := fmt.Sprintf("q%dm%d", n, m)
			bind[id] = qi
			queries = append(queries, cwtypes.MetricDataQuery{
				Id: aws.String(id),
				MetricStat: &cwtypes.MetricStat{
					Metric: &cwtypes.Metric{
						Namespace:  aws.String("AWS/SQS"),
						MetricName: aws.String(metric),
						Dimensions: []cwtypes.Dimension{{
							Name: aws.String("QueueName"), Value: aws.String(snap.Queues[qi].Name),
						}},
					},
					Period: aws.Int32(period),
					Stat:   aws.String("Sum"),
				},
			})
		}
	}

	end := snap.Now
	start := end.Add(-receiveActivityWindow)
	sums := make(map[string]float64, len(queries))
	for offset := 0; offset < len(queries); offset += maxQueriesPerCall {
		chunk := queries[offset:min(offset+maxQueriesPerCall, len(queries))]
		pager := awscloudwatch.NewGetMetricDataPaginator(client, &awscloudwatch.GetMetricDataInput{
			MetricDataQueries: chunk,
			StartTime:         aws.Time(start),
			EndTime:           aws.Time(end),
		})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				rec.record("cloudwatch", err)
				return // leave ReceiveActivityKnown=false: skip, don't guess
			}
			for _, r := range page.MetricDataResults {
				id := aws.ToString(r.Id)
				for _, v := range r.Values {
					sums[id] += v
				}
			}
		}
	}

	for id, qi := range bind {
		snap.Queues[qi].ReceiveActivityKnown = true
		snap.Queues[qi].Receives += sums[id]
	}
}

func collectTopicsMessaging(ctx context.Context, cfg aws.Config, snap *findings.MessagingSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awssns.NewFromConfig(cfg)

	var arns []string
	pager := awssns.NewListTopicsPaginator(client, &awssns.ListTopicsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("sns", err)
			return
		}
		for _, t := range page.Topics {
			arns = append(arns, aws.ToString(t.TopicArn))
		}
		if len(arns) >= maxTopicChecks {
			rec.recordTruncation("sns", "topics", maxTopicChecks)
			arns = arns[:maxTopicChecks]
			break
		}
	}

	for _, arn := range arns {
		name := arn
		if i := strings.LastIndexByte(arn, ':'); i >= 0 {
			name = arn[i+1:]
		}
		topic := findings.MsgTopic{ARN: arn, Name: name}
		out, err := client.GetTopicAttributes(ctx, &awssns.GetTopicAttributesInput{TopicArn: aws.String(arn)})
		if err != nil {
			rec.record("sns", err)
			snap.Topics = append(snap.Topics, topic)
			break
		}
		if v, ok := out.Attributes["SubscriptionsConfirmed"]; ok {
			n := int(atoi64(v))
			topic.SubscriptionsConfirmed = &n
		}
		if v, ok := out.Attributes["SubscriptionsPending"]; ok {
			n := int(atoi64(v))
			topic.SubscriptionsPending = &n
		}
		snap.Topics = append(snap.Topics, topic)
	}
}
