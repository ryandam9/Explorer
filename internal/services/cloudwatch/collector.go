package cloudwatch

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "cloudwatch"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := cloudwatch.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	// Without AlarmTypes, DescribeAlarms returns only metric alarms — composite
	// alarms come back in a separate field that must be requested explicitly, or
	// they're invisible (CLAUDE.md §5).
	paginator := cloudwatch.NewDescribeAlarmsPaginator(client, &cloudwatch.DescribeAlarmsInput{
		AlarmTypes: []types.AlarmType{types.AlarmTypeMetricAlarm, types.AlarmTypeCompositeAlarm},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe CloudWatch alarms: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.MetricAlarms)+len(page.CompositeAlarms))
		for _, alarm := range page.MetricAlarms {
			batch = append(batch, c.mapAlarm(input.Region, alarm, input.DetailLevel))
		}
		for _, alarm := range page.CompositeAlarms {
			batch = append(batch, c.mapCompositeAlarm(input.Region, alarm, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

// mapCompositeAlarm maps a composite alarm (one whose state is derived from a
// rule over other alarms). It has no metric/threshold of its own, so the
// summary carries its AlarmRule instead.
func (c *Collector) mapCompositeAlarm(region string, alarm types.CompositeAlarm, detail services.DetailLevel) model.Resource {
	id := aws.ToString(alarm.AlarmArn)
	res := model.Resource{
		Service: "cloudwatch",
		Type:    "composite-alarm",
		Region:  region,
		ID:      id,
		Name:    aws.ToString(alarm.AlarmName),
		ARN:     id,
		State:   string(alarm.StateValue),
		Summary: map[string]string{
			"alarmRule": aws.ToString(alarm.AlarmRule),
		},
	}
	if alarm.StateUpdatedTimestamp != nil {
		res.Summary["stateUpdated"] = alarm.StateUpdatedTimestamp.Format("2006-01-02 15:04:05")
	}
	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"alarmDescription":        aws.ToString(alarm.AlarmDescription),
			"actionsEnabled":          aws.ToBool(alarm.ActionsEnabled),
			"insufficientDataActions": alarm.InsufficientDataActions,
			"okActions":               alarm.OKActions,
			"alarmActions":            alarm.AlarmActions,
		}
	}
	return res
}

func (c *Collector) mapAlarm(region string, alarm types.MetricAlarm, detail services.DetailLevel) model.Resource {
	id := aws.ToString(alarm.AlarmArn)
	name := aws.ToString(alarm.AlarmName)
	state := string(alarm.StateValue)

	res := model.Resource{
		Service: "cloudwatch",
		Type:    "alarm",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id,
		State:   state,
		Summary: map[string]string{
			"metricName":         aws.ToString(alarm.MetricName),
			"namespace":          aws.ToString(alarm.Namespace),
			"statistic":          string(alarm.Statistic),
			"period":             fmt.Sprintf("%d", aws.ToInt32(alarm.Period)),
			"evaluationPeriods":  fmt.Sprintf("%d", aws.ToInt32(alarm.EvaluationPeriods)),
			"threshold":          fmt.Sprintf("%.1f", aws.ToFloat64(alarm.Threshold)),
			"comparisonOperator": string(alarm.ComparisonOperator),
		},
	}

	if alarm.StateUpdatedTimestamp != nil {
		res.Summary["stateUpdated"] = alarm.StateUpdatedTimestamp.Format("2006-01-02 15:04:05")
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"alarmDescription":        aws.ToString(alarm.AlarmDescription),
			"actionsEnabled":          aws.ToBool(alarm.ActionsEnabled),
			"insufficientDataActions": alarm.InsufficientDataActions,
			"okActions":               alarm.OKActions,
			"alarmActions":            alarm.AlarmActions,
		}
	}

	return res
}
