package lambdatui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// metricsAPI and logsAPI are the two calls the activity view makes, narrowed
// to interfaces so tests can stub them.
type metricsAPI interface {
	GetMetricData(ctx context.Context, in *cloudwatch.GetMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error)
}

type logsAPI interface {
	FilterLogEvents(ctx context.Context, in *cloudwatchlogs.FilterLogEventsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.FilterLogEventsOutput, error)
}

// ActivityQuery is one activity run: a function's day, and optionally a regex
// to scan its logs for.
type ActivityQuery struct {
	Region     string
	Function   string
	LogGroup   string
	Day        Day
	Pattern    *regexp.Regexp // nil = metrics only, no log scan
	Filter     string         // optional server-side CloudWatch Logs filter pattern
	MaxMatches int

	// The function's configuration, for the performance summary: the timeout
	// and memory size the REPORT figures are measured against, and the
	// architecture that sets the price (0/"" when unknown).
	TimeoutSec int32
	MemoryMB   int32
	Arch       string
}

// NewActivityQuery builds a run for fn: its region, log group and the
// configuration the performance summary is measured against.
func NewActivityQuery(fn Function, day Day, pattern *regexp.Regexp, filter string, maxMatches int) ActivityQuery {
	return ActivityQuery{
		Region: fn.Region, Function: fn.Name, LogGroup: fn.LogGroup,
		Day: day, Pattern: pattern, Filter: filter, MaxMatches: maxMatches,
		TimeoutSec: fn.TimeoutSec, MemoryMB: fn.MemoryMB, Arch: primaryArch(fn.Architectures),
	}
}

// Per-call deadlines: one metrics call, and one FilterLogEvents page (a scan is
// many pages, each with its own deadline, so a long day never trips a single
// shared timeout mid-sweep).
const (
	activityMetricsTimeout = 30 * time.Second
	activityPageTimeout    = 60 * time.Second
)

func (c *Client) metricsFor(region string) (metricsAPI, error) {
	if cl, ok := c.metrics[region]; ok {
		return cl, nil
	}
	return nil, fmt.Errorf("no CloudWatch client for region %s", region)
}

func (c *Client) logGroupsFor(region string) (logGroupsAPI, error) {
	if cl, ok := c.logGroups[region]; ok {
		return cl, nil
	}
	return nil, fmt.Errorf("no CloudWatch Logs client for region %s", region)
}

func (c *Client) logsFor(region string) (logsAPI, error) {
	if cl, ok := c.logs[region]; ok {
		return cl, nil
	}
	return nil, fmt.Errorf("no CloudWatch Logs client for region %s", region)
}

// InvocationStats fetches the day's AWS/Lambda Invocations, Errors and
// Throttles (Sum per hour) for a function in one batched GetMetricData call.
// The FunctionName dimension aggregates every version and alias.
func (c *Client) InvocationStats(ctx context.Context, region, function string, day Day) (InvocationStats, error) {
	cl, err := c.metricsFor(region)
	if err != nil {
		return InvocationStats{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, activityMetricsTimeout)
	defer cancel()

	ids := []string{"inv", "err", "thr"}
	names := []string{"Invocations", "Errors", "Throttles"}
	queries := make([]cwtypes.MetricDataQuery, 0, len(ids))
	for i, id := range ids {
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String("AWS/Lambda"),
					MetricName: aws.String(names[i]),
					Dimensions: []cwtypes.Dimension{{Name: aws.String("FunctionName"), Value: aws.String(function)}},
				},
				Period: aws.Int32(3600),
				Stat:   aws.String("Sum"),
			},
		})
	}

	series := map[string]*metricSeries{}
	for _, id := range ids {
		series[id] = &metricSeries{}
	}
	p := cloudwatch.NewGetMetricDataPaginator(cl, &cloudwatch.GetMetricDataInput{
		StartTime:         aws.Time(day.Start),
		EndTime:           aws.Time(day.End),
		MetricDataQueries: queries,
		ScanBy:            cwtypes.ScanByTimestampAscending,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return InvocationStats{}, err
		}
		for _, r := range page.MetricDataResults {
			s, ok := series[aws.ToString(r.Id)]
			if !ok {
				continue
			}
			if r.StatusCode == cwtypes.StatusCodeInternalError || r.StatusCode == cwtypes.StatusCodeForbidden {
				return InvocationStats{}, fmt.Errorf("GetMetricData %s: %s", aws.ToString(r.Label), r.StatusCode)
			}
			s.Timestamps = append(s.Timestamps, r.Timestamps...)
			s.Values = append(s.Values, r.Values...)
		}
	}
	return buildInvocationStats(day, *series["inv"], *series["err"], *series["thr"]), nil
}

// LogPage reads one FilterLogEvents page of the day's window. next is nil once
// the window is exhausted.
func (c *Client) LogPage(ctx context.Context, q ActivityQuery, token *string) (events []LogEvent, next *string, err error) {
	cl, err := c.logsFor(q.Region)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, activityPageTimeout)
	defer cancel()

	in := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName: aws.String(q.LogGroup),
		StartTime:    aws.Int64(q.Day.Start.UnixMilli()),
		EndTime:      aws.Int64(q.Day.End.UnixMilli() - 1), // EndTime is inclusive; the day is [Start, End)
		NextToken:    token,
	}
	if q.Filter != "" {
		in.FilterPattern = aws.String(q.Filter)
	}
	out, err := cl.FilterLogEvents(ctx, in)
	if err != nil {
		if apiErrorCodeIs(err, "ResourceNotFoundException") {
			return nil, nil, fmt.Errorf("log group %s does not exist (the function has not logged in this region, or logs elsewhere)", q.LogGroup)
		}
		return nil, nil, err
	}
	events = make([]LogEvent, 0, len(out.Events))
	for _, e := range out.Events {
		events = append(events, LogEvent{
			Time:    time.UnixMilli(aws.ToInt64(e.Timestamp)),
			Stream:  aws.ToString(e.LogStreamName),
			Message: aws.ToString(e.Message),
		})
	}
	return events, out.NextToken, nil
}

// ScanLogs runs a whole log scan (the CLI's loop; the TUI pages one message at
// a time instead). progress, when set, is called after every page. A failed
// page ends the scan with Err set; the matches read so far are kept.
func (c *Client) ScanLogs(ctx context.Context, q ActivityQuery, progress func(*LogScan)) *LogScan {
	scan := NewLogScan(q.Pattern, q.Filter, q.MaxMatches)
	var token *string
	for {
		events, next, err := c.LogPage(ctx, q, token)
		if err != nil {
			scan.Fail(err)
			break
		}
		more := scan.Ingest(events)
		if progress != nil {
			progress(scan)
		}
		if !more || !scan.Advance(next != nil) {
			break
		}
		token = next
	}
	scan.SortMatches()
	return scan
}

// ResolveFunction finds a function for the CLI by name or ARN: an ARN pins its
// own region; a bare name is looked up (GetFunctionConfiguration) across the
// regions in scope. It returns the function's region and log group (the
// LoggingConfig group when set). A name found in several regions is an error
// asking for --region rather than a guess.
//
// When the lookup is denied in a single-region scope it degrades to the
// default /aws/lambda/<name> group and returns a warning instead of failing:
// the metrics and log reads may still be permitted.
func (c *Client) ResolveFunction(ctx context.Context, nameOrARN string) (fn Function, warning string, err error) {
	name, regions := nameOrARN, c.regions
	if strings.HasPrefix(nameOrARN, "arn:") {
		parts := strings.Split(nameOrARN, ":")
		if len(parts) < 7 || parts[2] != "lambda" || parts[5] != "function" {
			return Function{}, "", fmt.Errorf("%q is not a Lambda function ARN", nameOrARN)
		}
		name, regions = parts[6], []string{parts[3]}
		if _, ok := c.clients[parts[3]]; !ok {
			return Function{}, "", fmt.Errorf("function %s is in %s, outside the regions in scope — pass --region %s", name, parts[3], parts[3])
		}
	}

	type result struct {
		fn    Function
		found bool
		err   error
	}
	results := make([]result, len(regions))
	var wg sync.WaitGroup
	for i, region := range regions {
		wg.Add(1)
		go func(i int, region string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, drillTimeout)
			defer cancel()
			out, err := c.clientFor(region).GetFunctionConfiguration(ctx, &lambda.GetFunctionConfigurationInput{FunctionName: aws.String(name)})
			switch {
			case err == nil:
				f := Function{Name: aws.ToString(out.FunctionName), Region: region, ARN: aws.ToString(out.FunctionArn),
					TimeoutSec: aws.ToInt32(out.Timeout), MemoryMB: aws.ToInt32(out.MemorySize)}
				for _, a := range out.Architectures {
					f.Architectures = append(f.Architectures, string(a))
				}
				if f.Name == "" {
					f.Name = name
				}
				f.LogGroup = "/aws/lambda/" + f.Name
				if out.LoggingConfig != nil && aws.ToString(out.LoggingConfig.LogGroup) != "" {
					f.LogGroup = aws.ToString(out.LoggingConfig.LogGroup)
				}
				results[i] = result{fn: f, found: true}
			case apiErrorCodeIs(err, "ResourceNotFoundException"):
				results[i] = result{}
			default:
				results[i] = result{err: fmt.Errorf("%s: %w", region, err)}
			}
		}(i, region)
	}
	wg.Wait()

	var found []Function
	var errs []error
	for _, r := range results {
		if r.found {
			found = append(found, r.fn)
		}
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}
	switch {
	case len(found) == 1:
		return found[0], "", nil
	case len(found) > 1:
		var rs []string
		for _, f := range found {
			rs = append(rs, f.Region)
		}
		sort.Strings(rs)
		return Function{}, "", fmt.Errorf("function %q exists in several regions (%s) — pass --region", name, strings.Join(rs, ", "))
	case len(errs) > 0 && len(regions) == 1:
		slog.Warn("Lambda GetFunctionConfiguration failed; assuming the default log group", "function", name, "error", errs[0].Error())
		return Function{Name: name, Region: regions[0], LogGroup: "/aws/lambda/" + name},
			fmt.Sprintf("could not read the function's configuration (%v); assuming log group /aws/lambda/%s — pass --log-group if it logs elsewhere", errs[0], name), nil
	case len(errs) > 0:
		return Function{}, "", fmt.Errorf("function %q not found; some regions could not be checked: %w", name, errors.Join(errs...))
	default:
		return Function{}, "", fmt.Errorf("function %q not found in %s", name, strings.Join(regions, ", "))
	}
}

// invocationMaxPages bounds the stream read behind the invocation drill-down.
// One stream holds one execution environment's runs back to back, so a window
// of ±timeout is at most a few thousand lines; the bound only guards a
// pathological chatty function.
const invocationMaxPages = 20

// InvocationLines reads the invocation a log line belongs to back from its
// stream: every event of the stream within ±timeout of the line (see
// invocationWindow), from which extractInvocation keeps that run's lines. It
// stops early once the run's REPORT line has been read. truncated is set when
// the page bound ended the read before the window was exhausted.
func (c *Client) InvocationLines(ctx context.Context, region, logGroup, stream, requestID string, at time.Time, timeout time.Duration) (inv Invocation, truncated bool, err error) {
	cl, err := c.logsFor(region)
	if err != nil {
		return Invocation{}, false, err
	}
	start, end := invocationWindow(at, timeout)
	var events []LogEvent
	var token *string
	for page := 0; ; page++ {
		if page >= invocationMaxPages {
			truncated = true
			break
		}
		pctx, cancel := context.WithTimeout(ctx, activityPageTimeout)
		out, err := cl.FilterLogEvents(pctx, &cloudwatchlogs.FilterLogEventsInput{
			LogGroupName:   aws.String(logGroup),
			LogStreamNames: []string{stream},
			StartTime:      aws.Int64(start.UnixMilli()),
			EndTime:        aws.Int64(end.UnixMilli()),
			NextToken:      token,
		})
		cancel()
		if err != nil {
			return Invocation{}, false, err
		}
		for _, e := range out.Events {
			events = append(events, LogEvent{
				Time:    time.UnixMilli(aws.ToInt64(e.Timestamp)),
				Stream:  aws.ToString(e.LogStreamName),
				Message: aws.ToString(e.Message),
			})
		}
		if out.NextToken == nil {
			break
		}
		// Once this run's REPORT is in, later pages hold only later runs.
		if extractInvocation(append([]LogEvent(nil), events...), requestID).HasEnd {
			break
		}
		token = out.NextToken
	}
	return extractInvocation(events, requestID), truncated, nil
}
