package cloudwatch

import (
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/user/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "cloudwatch" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cloudwatch")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — CloudWatch is a regional service")
	}
}

func TestMapAlarm_BasicFields(t *testing.T) {
	c := NewCollector()
	arn := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-cpu"
	alarm := types.MetricAlarm{
		AlarmArn:           aws.String(arn),
		AlarmName:          aws.String("high-cpu"),
		StateValue:         types.StateValueAlarm,
		MetricName:         aws.String("CPUUtilization"),
		Namespace:          aws.String("AWS/EC2"),
		Statistic:          types.StatisticAverage,
		Period:             aws.Int32(300),
		EvaluationPeriods:  aws.Int32(3),
		Threshold:          aws.Float64(80.0),
		ComparisonOperator: types.ComparisonOperatorGreaterThanOrEqualToThreshold,
	}

	res := c.mapAlarm("us-east-1", alarm, services.DetailLevelSummary)

	if res.Service != "cloudwatch" {
		t.Errorf("Service = %q, want %q", res.Service, "cloudwatch")
	}
	if res.Type != "alarm" {
		t.Errorf("Type = %q, want %q", res.Type, "alarm")
	}
	if res.ID != arn {
		t.Errorf("ID = %q, want %q", res.ID, arn)
	}
	if res.ARN != arn {
		t.Errorf("ARN = %q, want %q", res.ARN, arn)
	}
	if res.Name != "high-cpu" {
		t.Errorf("Name = %q, want %q", res.Name, "high-cpu")
	}
	if res.State != "ALARM" {
		t.Errorf("State = %q, want %q", res.State, "ALARM")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
}

func TestMapAlarm_SummaryFields(t *testing.T) {
	c := NewCollector()
	alarm := types.MetricAlarm{
		AlarmArn:           aws.String("arn:aws:cloudwatch:us-west-2:123:alarm:disk-usage"),
		AlarmName:          aws.String("disk-usage"),
		StateValue:         types.StateValueOk,
		MetricName:         aws.String("DiskReadOps"),
		Namespace:          aws.String("AWS/EBS"),
		Statistic:          types.StatisticSum,
		Period:             aws.Int32(60),
		EvaluationPeriods:  aws.Int32(1),
		Threshold:          aws.Float64(1000.5),
		ComparisonOperator: types.ComparisonOperatorGreaterThanThreshold,
	}

	res := c.mapAlarm("us-west-2", alarm, services.DetailLevelSummary)

	if res.Summary["metricName"] != "DiskReadOps" {
		t.Errorf("Summary[metricName] = %q", res.Summary["metricName"])
	}
	if res.Summary["namespace"] != "AWS/EBS" {
		t.Errorf("Summary[namespace] = %q", res.Summary["namespace"])
	}
	if res.Summary["statistic"] != "Sum" {
		t.Errorf("Summary[statistic] = %q, want %q", res.Summary["statistic"], "Sum")
	}
	if res.Summary["period"] != "60" {
		t.Errorf("Summary[period] = %q, want %q", res.Summary["period"], "60")
	}
	if res.Summary["evaluationPeriods"] != "1" {
		t.Errorf("Summary[evaluationPeriods] = %q, want %q", res.Summary["evaluationPeriods"], "1")
	}
	if res.Summary["threshold"] != "1000.5" {
		t.Errorf("Summary[threshold] = %q, want %q", res.Summary["threshold"], "1000.5")
	}
	if res.Summary["comparisonOperator"] != "GreaterThanThreshold" {
		t.Errorf("Summary[comparisonOperator] = %q", res.Summary["comparisonOperator"])
	}
}

func TestMapAlarm_WithStateUpdatedTimestamp(t *testing.T) {
	c := NewCollector()
	updated := time.Date(2024, 5, 10, 15, 30, 0, 0, time.UTC)
	alarm := types.MetricAlarm{
		AlarmArn:             aws.String("arn:aws:cloudwatch:us-east-1:123:alarm:ts-alarm"),
		AlarmName:            aws.String("ts-alarm"),
		StateValue:           types.StateValueOk,
		StateUpdatedTimestamp: &updated,
	}

	res := c.mapAlarm("us-east-1", alarm, services.DetailLevelSummary)

	if !strings.Contains(res.Summary["stateUpdated"], "2024-05-10") {
		t.Errorf("Summary[stateUpdated] = %q, expected to contain date", res.Summary["stateUpdated"])
	}
}

func TestMapAlarm_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	alarm := types.MetricAlarm{
		AlarmArn:  aws.String("arn:aws:cloudwatch:us-east-1:123:alarm:no-detail"),
		AlarmName: aws.String("no-detail"),
	}

	res := c.mapAlarm("us-east-1", alarm, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapAlarm_DetailLevel(t *testing.T) {
	c := NewCollector()
	alarm := types.MetricAlarm{
		AlarmArn:        aws.String("arn:aws:cloudwatch:eu-central-1:123:alarm:detail-alarm"),
		AlarmName:       aws.String("detail-alarm"),
		StateValue:      types.StateValueInsufficientData,
		AlarmDescription: aws.String("triggers on high latency"),
		ActionsEnabled:  aws.Bool(true),
		AlarmActions:    []string{"arn:aws:sns:eu-central-1:123:alert-topic"},
	}

	res := c.mapAlarm("eu-central-1", alarm, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["alarmDescription"] != "triggers on high latency" {
		t.Errorf("Details[alarmDescription] = %v", res.Details["alarmDescription"])
	}
	if res.Details["actionsEnabled"] != true {
		t.Errorf("Details[actionsEnabled] = %v, want true", res.Details["actionsEnabled"])
	}
}
