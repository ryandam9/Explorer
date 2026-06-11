package awsutil

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// FetchRecentLogs gets the last N lines matching the error pattern from the given log group.
func FetchRecentLogs(ctx context.Context, cfg aws.Config, region, logGroupName, pattern string) ([]string, error) {
	cwlCfg := cfg.Copy()
	if region != "" && region != "global" {
		cwlCfg.Region = region
	}
	client := cloudwatchlogs.NewFromConfig(cwlCfg)

	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(logGroupName),
		Limit:        aws.Int32(20),
	}
	if pattern != "" {
		input.FilterPattern = aws.String(pattern)
	}

	resp, err := client.FilterLogEvents(ctx, input)
	if err != nil {
		return nil, err
	}

	var lines []string
	for _, ev := range resp.Events {
		lines = append(lines, strings.TrimSpace(aws.ToString(ev.Message)))
	}
	return lines, nil
}

// SparklineMetric holds the numeric values for drawing the graph.
type SparklineMetric struct {
	Name   string
	Values []float64
	Unit   string
}

// FetchMetricData queries CloudWatch for a 1-hour timeline of the specified metric.
func FetchMetricData(ctx context.Context, cfg aws.Config, region, namespace, metricName string, dimensions map[string]string) (*SparklineMetric, error) {
	cwCfg := cfg.Copy()
	if region != "" && region != "global" {
		cwCfg.Region = region
	}
	client := cloudwatch.NewFromConfig(cwCfg)

	now := time.Now()
	startTime := now.Add(-1 * time.Hour)

	var dims []cwtypes.Dimension
	for k, v := range dimensions {
		dims = append(dims, cwtypes.Dimension{
			Name:  aws.String(k),
			Value: aws.String(v),
		})
	}

	input := &cloudwatch.GetMetricDataInput{
		StartTime: aws.Time(startTime),
		EndTime:   aws.Time(now),
		MetricDataQueries: []cwtypes.MetricDataQuery{
			{
				Id: aws.String("m1"),
				MetricStat: &cwtypes.MetricStat{
					Metric: &cwtypes.Metric{
						Namespace:  aws.String(namespace),
						MetricName: aws.String(metricName),
						Dimensions: dims,
					},
					Period: aws.Int32(300), // 5-minute intervals for 1-hour window (12 points)
					Stat:   aws.String("Average"),
				},
			},
		},
	}

	resp, err := client.GetMetricData(ctx, input)
	if err != nil {
		return nil, err
	}

	var values []float64
	if len(resp.MetricDataResults) > 0 {
		res := resp.MetricDataResults[0]
		// CloudWatch might return results out of chronological order (usually reverse)
		// We want to sort them by timestamp.
		type dp struct {
			t time.Time
			v float64
		}
		var points []dp
		for i, val := range res.Values {
			if i < len(res.Timestamps) {
				points = append(points, dp{t: res.Timestamps[i], v: val})
			}
		}
		// Sort ascending by timestamp
		for i := 0; i < len(points); i++ {
			for j := i + 1; j < len(points); j++ {
				if points[i].t.After(points[j].t) {
					points[i], points[j] = points[j], points[i]
				}
			}
		}
		for _, p := range points {
			values = append(values, p.v)
		}
	}

	return &SparklineMetric{
		Name:   metricName,
		Values: values,
		Unit:   "Avg",
	}, nil
}

// GenerateSparkline renders a Unicode sparkline.
func GenerateSparkline(data []float64) string {
	if len(data) == 0 {
		return "No data points"
	}
	minVal := data[0]
	maxVal := data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	span := maxVal - minVal
	// 8 levels of block heights
	blocks := []rune{' ', ' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	var sb strings.Builder
	for _, v := range data {
		if span == 0 {
			sb.WriteRune(blocks[4]) // use middle height block if flat
			continue
		}
		idx := int(((v - minVal) / span) * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}
	return sb.String()
}
