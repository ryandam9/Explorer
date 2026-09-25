package lambdatui

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cwltypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
)

func TestUsageMetrics(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	d1, d2 := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	stub := &stubMetrics{out: &cloudwatch.GetMetricDataOutput{MetricDataResults: []cwtypes.MetricDataResult{
		{Id: aws.String("f0"), Timestamps: []time.Time{d2, d1}, Values: []float64{3, 4}, StatusCode: cwtypes.StatusCodeComplete},
		{Id: aws.String("f1"), StatusCode: cwtypes.StatusCodeComplete}, // idle: no datapoints
		{Id: aws.String("f2"), StatusCode: cwtypes.StatusCodeForbidden},
	}}}
	fns := []Function{{Name: "busy", Region: "r"}, {Name: "idle", Region: "r"}, {Name: "denied", Region: "r"}}
	out := map[string]*FunctionUsage{}
	for _, f := range fns {
		out[usageKey(f.Region, f.Name)] = &FunctionUsage{}
	}
	var notes noteSet
	usageMetrics(context.Background(), stub, fns, now, out, &notes)

	if u := out["r/busy"]; !u.UsageKnown || u.Invocations30d != 7 || !u.LastInvoked.Equal(d2) {
		t.Errorf("busy = %+v", u)
	}
	if u := out["r/idle"]; !u.UsageKnown || u.Invocations30d != 0 || !u.LastInvoked.IsZero() {
		t.Errorf("no datapoint is a measured 0: %+v", u)
	}
	if u := out["r/denied"]; u.UsageKnown {
		t.Errorf("a forbidden query must stay unknown: %+v", u)
	}
	if len(notes.list()) != 1 {
		t.Errorf("notes = %v", notes.list())
	}
	in := stub.in
	if in == nil || len(in.MetricDataQueries) != 3 || aws.ToInt32(in.MetricDataQueries[0].MetricStat.Period) != 86400 ||
		!in.StartTime.Equal(time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("query = %+v", in)
	}
}

type stubLogGroups struct{ calls []string }

func (s *stubLogGroups) DescribeLogGroups(_ context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	prefix := aws.ToString(in.LogGroupNamePrefix)
	s.calls = append(s.calls, prefix)
	if prefix == "/aws/lambda/" {
		return &cloudwatchlogs.DescribeLogGroupsOutput{LogGroups: []cwltypes.LogGroup{
			{LogGroupName: aws.String("/aws/lambda/keep"), RetentionInDays: aws.Int32(30), StoredBytes: aws.Int64(10)},
			{LogGroupName: aws.String("/aws/lambda/forever"), StoredBytes: aws.Int64(5 << 20)},
		}}, nil
	}
	return &cloudwatchlogs.DescribeLogGroupsOutput{LogGroups: []cwltypes.LogGroup{{LogGroupName: aws.String("/custom/app"), StoredBytes: aws.Int64(1)}}}, nil
}

func TestUsageLogGroups(t *testing.T) {
	fns := []Function{
		{Name: "keep", Region: "r", LogGroup: "/aws/lambda/keep"},
		{Name: "forever", Region: "r", LogGroup: "/aws/lambda/forever"},
		{Name: "silent", Region: "r", LogGroup: "/aws/lambda/silent"},
		{Name: "custom", Region: "r", LogGroup: "/custom/app"},
	}
	out := map[string]*FunctionUsage{}
	for _, f := range fns {
		out[usageKey(f.Region, f.Name)] = &FunctionUsage{}
	}
	api := &stubLogGroups{}
	usageLogGroups(context.Background(), api, fns, out, &noteSet{})
	if u := out["r/keep"]; !u.LogKnown || !u.LogExists || u.RetentionDays != 30 {
		t.Errorf("keep = %+v", u)
	}
	if u := out["r/forever"]; !u.LogExists || u.RetentionDays != 0 || u.StoredBytes != 5<<20 {
		t.Errorf("forever = %+v", u)
	}
	if u := out["r/silent"]; !u.LogKnown || u.LogExists {
		t.Errorf("a function that never logged has no group (known): %+v", u)
	}
	if u := out["r/custom"]; !u.LogExists || len(api.calls) != 2 {
		t.Errorf("custom group needs its own lookup: %+v calls=%v", u, api.calls)
	}
}

type stubPosture struct{}

func (stubPosture) GetPolicy(_ context.Context, in *lambda.GetPolicyInput, _ ...func(*lambda.Options)) (*lambda.GetPolicyOutput, error) {
	switch aws.ToString(in.FunctionName) {
	case "open":
		return &lambda.GetPolicyOutput{Policy: aws.String(`{"Statement":[]}`)}, nil
	case "none":
		return nil, &smithy.GenericAPIError{Code: "ResourceNotFoundException"}
	}
	return nil, &smithy.GenericAPIError{Code: "AccessDeniedException", Message: "denied"}
}

func (stubPosture) ListFunctionUrlConfigs(_ context.Context, in *lambda.ListFunctionUrlConfigsInput, _ ...func(*lambda.Options)) (*lambda.ListFunctionUrlConfigsOutput, error) {
	if aws.ToString(in.FunctionName) == "open" {
		return &lambda.ListFunctionUrlConfigsOutput{FunctionUrlConfigs: []lambdatypes.FunctionUrlConfig{{AuthType: lambdatypes.FunctionUrlAuthTypeNone}}}, nil
	}
	return &lambda.ListFunctionUrlConfigsOutput{}, nil
}

// Twenty denied GetPolicy calls collapse into one note; "no policy" is known.
func TestUsagePostureCollapsesErrors(t *testing.T) {
	fns := []Function{{Name: "open", Region: "r"}, {Name: "none", Region: "r"}}
	for i := 0; i < 20; i++ {
		fns = append(fns, Function{Name: "denied" + string(rune('a'+i)), Region: "r"})
	}
	out := map[string]*FunctionUsage{}
	for _, f := range fns {
		out[usageKey(f.Region, f.Name)] = &FunctionUsage{}
	}
	var notes noteSet
	usagePosture(context.Background(), stubPosture{}, fns, out, &notes)
	if u := out["r/open"]; !u.PolicyKnown || u.Policy == "" || !u.URLKnown || len(u.URLAuthTypes) != 1 {
		t.Errorf("open = %+v", u)
	}
	if u := out["r/none"]; !u.PolicyKnown || u.Policy != "" {
		t.Errorf("no policy is a known fact: %+v", u)
	}
	if u := out["r/denieda"]; u.PolicyKnown {
		t.Errorf("a denied read stays unknown: %+v", u)
	}
	if l := notes.list(); len(l) != 1 || l[0] != "couldn't read resource policies (lambda:GetPolicy) — access denied for 20 function(s)" {
		t.Errorf("notes = %q", l)
	}
}

func TestCompareCellsNumeric(t *testing.T) {
	if compareCells("128 MB", "1,024 MB") >= 0 || compareCells("99", "1,234") >= 0 || compareCells("?", "5") == 0 {
		t.Error("numeric cells should compare by value")
	}
	if compareCells("alpha", "Beta") >= 0 {
		t.Error("text compares case-insensitively")
	}
}
