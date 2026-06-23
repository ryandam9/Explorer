package xref

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	awscwl "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

// Observability edge extractors (#344): CloudWatch alarm actions and CloudWatch
// Logs log-group subscriptions / encryption. Pure mapping functions are
// fixture-tested; the wrappers page and delegate.

// --- CloudWatch alarms --------------------------------------------------------

// alarmActionEdges maps an alarm's action ARNs (alarm/OK/insufficient-data) to
// "alarm action" edges. Non-ARN actions are skipped.
func alarmActionEdges(from Reference, lists ...[]string) []Edge {
	var edges []Edge
	for _, list := range lists {
		for _, act := range list {
			if isARN(act) {
				edges = append(edges, Edge{From: withVia(from, "alarm action"), Target: act})
			}
		}
	}
	return edges
}

func metricAlarmEdges(a cwtypes.MetricAlarm, region string) []Edge {
	from := Reference{Service: "cloudwatch", Type: "alarm", Region: region,
		ID: aws.ToString(a.AlarmArn), Name: aws.ToString(a.AlarmName)}
	return alarmActionEdges(from, a.AlarmActions, a.OKActions, a.InsufficientDataActions)
}

func compositeAlarmEdges(a cwtypes.CompositeAlarm, region string) []Edge {
	from := Reference{Service: "cloudwatch", Type: "composite-alarm", Region: region,
		ID: aws.ToString(a.AlarmArn), Name: aws.ToString(a.AlarmName)}
	return alarmActionEdges(from, a.AlarmActions, a.OKActions, a.InsufficientDataActions)
}

func cloudWatchAlarmEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awscw.NewFromConfig(cfg)
	var edges []Edge
	p := awscw.NewDescribeAlarmsPaginator(client, &awscw.DescribeAlarmsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("cloudwatch", err)
			break
		}
		for _, a := range page.MetricAlarms {
			edges = append(edges, metricAlarmEdges(a, region)...)
		}
		for _, a := range page.CompositeAlarms {
			edges = append(edges, compositeAlarmEdges(a, region)...)
		}
	}
	return edges
}

// --- CloudWatch Logs ----------------------------------------------------------

// logGroupRef identifies a log group by its name so it unifies with the
// derived Lambda/ECS log-group edges (which target the name, not the ARN).
func logGroupRef(lg cwltypes.LogGroup, region string) Reference {
	name := aws.ToString(lg.LogGroupName)
	return Reference{Service: "logs", Type: "log-group", Region: region, ID: name, Name: name}
}

// subscriptionFilterEdges maps a log group's subscription filters to their
// destinations (Lambda / Kinesis / Firehose).
func subscriptionFilterEdges(from Reference, filters []cwltypes.SubscriptionFilter) []Edge {
	var edges []Edge
	for _, f := range filters {
		if d := aws.ToString(f.DestinationArn); d != "" {
			edges = append(edges, Edge{From: withVia(from, "subscription filter"), Target: d})
		}
	}
	return edges
}

func cwLogsEdges(ctx context.Context, cfg aws.Config, region string, maxConcurrency int, rec *recorder) []Edge {
	client := awscwl.NewFromConfig(cfg)
	var groups []cwltypes.LogGroup
	p := awscwl.NewDescribeLogGroupsPaginator(client, &awscwl.DescribeLogGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("logs", err)
			break
		}
		groups = append(groups, page.LogGroups...)
	}
	// One DescribeSubscriptionFilters per log group — accounts can have hundreds,
	// so fan out instead of calling them serially under the region deadline (§7).
	return boundedEdges(ctx, groups, maxConcurrency, rec, func(ctx context.Context, lg cwltypes.LogGroup, rec *recorder) []Edge {
		ref := logGroupRef(lg, region)
		var edges []Edge
		if key := aws.ToString(lg.KmsKeyId); key != "" {
			edges = append(edges, Edge{From: withVia(ref, "log group encryption key"), Target: key})
		}
		out, err := client.DescribeSubscriptionFilters(ctx, &awscwl.DescribeSubscriptionFiltersInput{LogGroupName: lg.LogGroupName})
		if err != nil {
			rec.record("logs", err)
			return edges
		}
		return append(edges, subscriptionFilterEdges(ref, out.SubscriptionFilters)...)
	})
}

func observabilityEdges(ctx context.Context, cfg aws.Config, region string, maxConcurrency int, rec *recorder) []Edge {
	var edges []Edge
	edges = append(edges, cloudWatchAlarmEdges(ctx, cfg, region, rec)...)
	edges = append(edges, cwLogsEdges(ctx, cfg, region, maxConcurrency, rec)...)
	return edges
}
