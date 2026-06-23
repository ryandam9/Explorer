package xref

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseb "github.com/aws/aws-sdk-go-v2/service/eventbridge"
	ebtypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	awskinesis "github.com/aws/aws-sdk-go-v2/service/kinesis"
	awssfn "github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// Messaging & event-wiring edge extractors (#342): SNS subscriptions, SQS
// dead-letter redrive, EventBridge rule targets, Step Functions definition
// references, and Kinesis stream encryption/consumers. The mapping logic is in
// pure functions; the AWS-calling wrappers page and delegate.

// --- SNS ----------------------------------------------------------------------

// snsSubscriptionEdges maps SNS subscriptions to "topic → endpoint" edges
// (SQS/Lambda/Firehose by ARN; HTTP(S)/email by their endpoint string).
func snsSubscriptionEdges(subs []snstypes.Subscription, region string) []Edge {
	var edges []Edge
	for _, s := range subs {
		topic := aws.ToString(s.TopicArn)
		endpoint := aws.ToString(s.Endpoint)
		if topic == "" || endpoint == "" {
			continue
		}
		from := Reference{Service: "sns", Type: "topic", Region: region,
			ID: topic, Name: lastSegment(shortForm(topic))}
		via := "SNS subscription"
		if p := aws.ToString(s.Protocol); p != "" {
			via = "SNS subscription (" + p + ")"
		}
		edges = append(edges, Edge{From: withVia(from, via), Target: endpoint})
	}
	return edges
}

func snsEdges(ctx context.Context, cfg aws.Config, region string, maxConcurrency int, rec *recorder) []Edge {
	client := awssns.NewFromConfig(cfg)
	var edges []Edge

	// Subscriptions are listed account-wide in one paginated sweep (no per-topic
	// fan-out).
	sp := awssns.NewListSubscriptionsPaginator(client, &awssns.ListSubscriptionsInput{})
	for sp.HasMorePages() {
		page, err := sp.NextPage(ctx)
		if err != nil {
			rec.record("sns", err)
			break
		}
		edges = append(edges, snsSubscriptionEdges(page.Subscriptions, region)...)
	}

	// Topic encryption keys need a per-topic attribute read.
	var topics []string
	tp := awssns.NewListTopicsPaginator(client, &awssns.ListTopicsInput{})
	for tp.HasMorePages() {
		page, err := tp.NextPage(ctx)
		if err != nil {
			rec.record("sns", err)
			break
		}
		for _, t := range page.Topics {
			if arn := aws.ToString(t.TopicArn); arn != "" {
				topics = append(topics, arn)
			}
		}
	}
	// One GetTopicAttributes per topic — fan out (§7).
	topicEdges := boundedEdges(ctx, topics, maxConcurrency, rec, func(ctx context.Context, arn string, rec *recorder) []Edge {
		attrs, err := client.GetTopicAttributes(ctx, &awssns.GetTopicAttributesInput{TopicArn: aws.String(arn)})
		if err != nil {
			rec.record("sns", err)
			return nil
		}
		if key := attrs.Attributes["KmsMasterKeyId"]; key != "" {
			from := Reference{Service: "sns", Type: "topic", Region: region, ID: arn, Name: lastSegment(shortForm(arn))}
			return []Edge{{From: withVia(from, "topic encryption key"), Target: key}}
		}
		return nil
	})
	return append(edges, topicEdges...)
}

// --- SQS dead-letter ----------------------------------------------------------

// sqsRedriveTarget extracts the dead-letter target ARN from a queue's
// RedrivePolicy attribute (JSON), "" when unset or unparseable.
func sqsRedriveTarget(policy string) string {
	if strings.TrimSpace(policy) == "" {
		return ""
	}
	var p struct {
		DeadLetterTargetArn string `json:"deadLetterTargetArn"`
	}
	if err := json.Unmarshal([]byte(policy), &p); err != nil {
		return ""
	}
	return p.DeadLetterTargetArn
}

// --- EventBridge --------------------------------------------------------------

// eventBridgeTargetEdges maps a rule's targets to "rule → target" edges, plus a
// dead-letter edge for any target configured with one.
func eventBridgeTargetEdges(ruleRef Reference, targets []ebtypes.Target) []Edge {
	var edges []Edge
	for _, t := range targets {
		if arn := aws.ToString(t.Arn); arn != "" {
			edges = append(edges, Edge{From: withVia(ruleRef, "EventBridge target"), Target: arn})
		}
		if t.DeadLetterConfig != nil {
			if dlq := aws.ToString(t.DeadLetterConfig.Arn); dlq != "" {
				edges = append(edges, Edge{From: withVia(ruleRef, "EventBridge dead-letter"), Target: dlq})
			}
		}
	}
	return edges
}

func eventBridgeEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awseb.NewFromConfig(cfg)
	var edges []Edge

	buses := []string{""} // "" = the default bus
	var token *string
	for {
		out, err := client.ListEventBuses(ctx, &awseb.ListEventBusesInput{NextToken: token})
		if err != nil {
			rec.record("events", err)
			break
		}
		for _, b := range out.EventBuses {
			if n := aws.ToString(b.Name); n != "" && n != "default" {
				buses = append(buses, n)
			}
		}
		if out.NextToken == nil {
			break
		}
		token = out.NextToken
	}

	for _, bus := range buses {
		var rtoken *string
		for {
			in := &awseb.ListRulesInput{NextToken: rtoken}
			if bus != "" {
				in.EventBusName = aws.String(bus)
			}
			page, err := client.ListRules(ctx, in)
			if err != nil {
				rec.record("events", err)
				break
			}
			for _, rule := range page.Rules {
				ruleRef := Reference{Service: "events", Type: "rule", Region: region,
					ID: aws.ToString(rule.Arn), Name: aws.ToString(rule.Name)}
				if eb := aws.ToString(rule.EventBusName); eb != "" && eb != "default" {
					edges = append(edges, Edge{From: withVia(ruleRef, "event bus"), Target: eb})
				}
				tin := &awseb.ListTargetsByRuleInput{Rule: rule.Name}
				if bus != "" {
					tin.EventBusName = aws.String(bus)
				}
				tout, err := client.ListTargetsByRule(ctx, tin)
				if err != nil {
					rec.record("events", err)
					continue
				}
				edges = append(edges, eventBridgeTargetEdges(ruleRef, tout.Targets)...)
			}
			if page.NextToken == nil {
				break
			}
			rtoken = page.NextToken
		}
	}
	return edges
}

// --- Step Functions -----------------------------------------------------------

var arnPattern = regexp.MustCompile(`arn:aws[a-z0-9-]*:[a-z0-9-]+:[a-z0-9-]*:[0-9]*:[^"'\s\\,}\]]+`)

// sfnDefinitionARNs extracts the distinct AWS ARNs referenced in a state
// machine's Amazon States Language definition (Resource fields, Parameters).
// Conservative: it only pulls out ARN-shaped tokens, never parameter values, so
// no secret-looking input is surfaced (§14). selfARN is excluded.
func sfnDefinitionARNs(definition, selfARN string) []string {
	matches := arnPattern.FindAllString(definition, -1)
	seen := map[string]bool{selfARN: true}
	var out []string
	for _, m := range matches {
		m = strings.TrimRight(m, `.`)
		if seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

func sfnEdges(ctx context.Context, cfg aws.Config, region string, maxConcurrency int, rec *recorder) []Edge {
	client := awssfn.NewFromConfig(cfg)
	var machines []sfntypes.StateMachineListItem
	p := awssfn.NewListStateMachinesPaginator(client, &awssfn.ListStateMachinesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("states", err)
			break
		}
		machines = append(machines, page.StateMachines...)
	}
	// One DescribeStateMachine per machine — fan out (§7).
	return boundedEdges(ctx, machines, maxConcurrency, rec, func(ctx context.Context, sm sfntypes.StateMachineListItem, rec *recorder) []Edge {
		arn := aws.ToString(sm.StateMachineArn)
		out, err := client.DescribeStateMachine(ctx, &awssfn.DescribeStateMachineInput{StateMachineArn: sm.StateMachineArn})
		if err != nil {
			rec.record("states", err)
			return nil
		}
		from := Reference{Service: "states", Type: "state-machine", Region: region,
			ID: arn, Name: aws.ToString(sm.Name)}
		var edges []Edge
		if role := aws.ToString(out.RoleArn); role != "" {
			edges = append(edges, Edge{From: withVia(from, "Step Functions execution role"), Target: role})
		}
		for _, ref := range sfnDefinitionARNs(aws.ToString(out.Definition), arn) {
			edges = append(edges, Edge{From: withVia(from, "state machine definition"), Target: ref})
		}
		return edges
	})
}

// --- Kinesis ------------------------------------------------------------------

func kinesisEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awskinesis.NewFromConfig(cfg)
	var edges []Edge
	p := awskinesis.NewListStreamsPaginator(client, &awskinesis.ListStreamsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("kinesis", err)
			break
		}
		for _, name := range page.StreamNames {
			sum, err := client.DescribeStreamSummary(ctx, &awskinesis.DescribeStreamSummaryInput{StreamName: aws.String(name)})
			if err != nil || sum.StreamDescriptionSummary == nil {
				if err != nil {
					rec.record("kinesis", err)
				}
				continue
			}
			d := sum.StreamDescriptionSummary
			arn := aws.ToString(d.StreamARN)
			from := Reference{Service: "kinesis", Type: "stream", Region: region, ID: arn, Name: name}
			if key := aws.ToString(d.KeyId); key != "" && key != "alias/aws/kinesis" {
				edges = append(edges, Edge{From: withVia(from, "stream encryption key"), Target: key})
			}
			cp := awskinesis.NewListStreamConsumersPaginator(client, &awskinesis.ListStreamConsumersInput{StreamARN: d.StreamARN})
			for cp.HasMorePages() {
				cPage, err := cp.NextPage(ctx)
				if err != nil {
					rec.record("kinesis", err)
					break
				}
				for _, c := range cPage.Consumers {
					if ca := aws.ToString(c.ConsumerARN); ca != "" {
						edges = append(edges, Edge{From: withVia(from, "registered consumer"), Target: ca})
					}
				}
			}
		}
	}
	return edges
}
