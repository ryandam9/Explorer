package lambdatui

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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

// Usage and posture, gathered in the background once the function list is on
// screen: 30 days of invocations per function (the INVOKES 30D / LAST INVOKED
// columns and the idle/arm64 checks), each function's log group retention
// (the never-expires check), and who can invoke it — its resource policy and
// function URLs (the public-access checks).
//
// Cost: the invocation counts are one GetMetricData call per region per 500
// functions, billed per metric requested — about $0.00001 per function per
// load, and nothing auto-refreshes. The log-group, policy and URL reads are
// free control-plane calls; the per-function ones run in a bounded pool.

// FunctionUsage is what was learned about one function. Each *Known flag is
// false when that read was denied or failed, which silences the checks that
// depend on it (a missing fact is never read as "no").
type FunctionUsage struct {
	UsageKnown     bool
	Invocations30d float64
	LastInvoked    time.Time // start of the last UTC day with an invocation (zero = none in 30 days)
	Daily          []float64 // invocations per UTC day, oldest first (NaN = no datapoint), for the sparkline

	LogKnown      bool
	LogExists     bool
	RetentionDays int32 // 0 = never expire
	StoredBytes   int64

	PolicyKnown  bool
	Policy       string // "" = no resource policy
	URLKnown     bool
	URLAuthTypes []string
}

// UsageReport is the background load's result: per-function facts keyed by
// region/name, and one note per read that failed for some functions.
type UsageReport struct {
	ByKey map[string]*FunctionUsage
	Notes []string
}

func usageKey(region, name string) string { return region + "/" + name }

// usageDays is the invocation lookback.
const usageDays = 30

// usageWorkers bounds the per-function policy/URL reads (Lambda's
// control-plane rate limits are per account; the SDK retries throttles).
const usageWorkers = 10

// metricQueriesPerCall is GetMetricData's limit on queries per request.
const metricQueriesPerCall = 500

type logGroupsAPI interface {
	DescribeLogGroups(ctx context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, optFns ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
}

type postureAPI interface {
	GetPolicy(ctx context.Context, in *lambda.GetPolicyInput, optFns ...func(*lambda.Options)) (*lambda.GetPolicyOutput, error)
	lambda.ListFunctionUrlConfigsAPIClient
}

// noteSet collapses per-function failures into one line per read, so fifty
// denied GetPolicy calls read as one actionable note, not fifty.
type noteSet struct {
	mu     sync.Mutex
	counts map[string]int
	sample map[string]string
}

func (n *noteSet) add(what string, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.counts == nil {
		n.counts, n.sample = map[string]int{}, map[string]string{}
	}
	reason := err.Error()
	if apiErrorCodeIs(err, "AccessDeniedException", "AccessDenied") {
		reason = "access denied"
	}
	key := what + " — " + reason
	n.counts[key]++
	n.sample[key] = err.Error()
}

func (n *noteSet) list() []string {
	var out []string
	for k, c := range n.counts {
		out = append(out, fmt.Sprintf("couldn't read %s for %d function(s)", k, c))
		slog.Warn("Lambda usage read failed", "read", k, "functions", c, "example", n.sample[k])
	}
	sort.Strings(out)
	return out
}

// LoadUsage gathers usage and posture for fns, region by region in parallel.
func (c *Client) LoadUsage(ctx context.Context, fns []Function, now time.Time) UsageReport {
	rep := UsageReport{ByKey: map[string]*FunctionUsage{}}
	byRegion := map[string][]Function{}
	for _, f := range fns {
		byRegion[f.Region] = append(byRegion[f.Region], f)
		rep.ByKey[usageKey(f.Region, f.Name)] = &FunctionUsage{}
	}
	var notes noteSet
	var wg sync.WaitGroup
	for region, rf := range byRegion {
		mapi, merr := c.metricsFor(region)
		lg, lerr := c.logGroupsFor(region)
		pa := c.clientFor(region)
		wg.Add(3)
		go func() {
			defer wg.Done()
			if merr != nil {
				notes.add("invocation counts", merr)
				return
			}
			usageMetrics(ctx, mapi, rf, now, rep.ByKey, &notes)
		}()
		go func() {
			defer wg.Done()
			if lerr != nil {
				notes.add("log groups", lerr)
				return
			}
			usageLogGroups(ctx, lg, rf, rep.ByKey, &notes)
		}()
		go func() {
			defer wg.Done()
			if pa == nil {
				return
			}
			usagePosture(ctx, pa, rf, rep.ByKey, &notes)
		}()
	}
	wg.Wait()
	rep.Notes = notes.list()
	return rep
}

// usageMetrics reads each function's daily Invocations for the last 30 days,
// 500 functions per GetMetricData call. A function with no datapoint was not
// invoked (Lambda publishes nothing for an idle day), which is a known 0 —
// unlike a failed call, which leaves the chunk unknown.
func usageMetrics(ctx context.Context, api metricsAPI, fns []Function, now time.Time, out map[string]*FunctionUsage, notes *noteSet) {
	end := now.UTC()
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1-usageDays)
	for lo := 0; lo < len(fns); lo += metricQueriesPerCall {
		chunk := fns[lo:min(lo+metricQueriesPerCall, len(fns))]
		queries := make([]cwtypes.MetricDataQuery, len(chunk))
		for i, f := range chunk {
			queries[i] = cwtypes.MetricDataQuery{
				Id: aws.String(fmt.Sprintf("f%d", i)),
				MetricStat: &cwtypes.MetricStat{
					Metric: &cwtypes.Metric{Namespace: aws.String("AWS/Lambda"), MetricName: aws.String("Invocations"),
						Dimensions: []cwtypes.Dimension{{Name: aws.String("FunctionName"), Value: aws.String(f.Name)}}},
					Period: aws.Int32(86400), Stat: aws.String("Sum"),
				},
			}
		}
		sums := make([]float64, len(chunk))
		last := make([]time.Time, len(chunk))
		daily := make([][]float64, len(chunk))
		for i := range daily {
			daily[i] = make([]float64, usageDays)
			for d := range daily[i] {
				daily[i][d] = math.NaN()
			}
		}
		failed := map[int]bool{}
		cctx, cancel := context.WithTimeout(ctx, activityMetricsTimeout)
		p := cloudwatch.NewGetMetricDataPaginator(api, &cloudwatch.GetMetricDataInput{
			StartTime: aws.Time(start), EndTime: aws.Time(end), MetricDataQueries: queries,
		})
		var err error
		for p.HasMorePages() {
			var page *cloudwatch.GetMetricDataOutput
			if page, err = p.NextPage(cctx); err != nil {
				break
			}
			for _, r := range page.MetricDataResults {
				var i int
				if _, scanErr := fmt.Sscanf(aws.ToString(r.Id), "f%d", &i); scanErr != nil || i < 0 || i >= len(chunk) {
					continue
				}
				if r.StatusCode == cwtypes.StatusCodeForbidden || r.StatusCode == cwtypes.StatusCodeInternalError {
					failed[i] = true
					continue
				}
				for k, v := range r.Values {
					sums[i] += v
					if k >= len(r.Timestamps) {
						continue
					}
					if d := int(r.Timestamps[k].Sub(start).Hours() / 24); d >= 0 && d < usageDays {
						if math.IsNaN(daily[i][d]) {
							daily[i][d] = 0
						}
						daily[i][d] += v
					}
					if v > 0 && r.Timestamps[k].After(last[i]) {
						last[i] = r.Timestamps[k]
					}
				}
			}
		}
		cancel()
		if err != nil {
			notes.add("invocation counts (cloudwatch:GetMetricData)", err)
			continue
		}
		for i, f := range chunk {
			if failed[i] {
				notes.add("invocation counts", fmt.Errorf("metric query status %s", "forbidden/internal error"))
				continue
			}
			u := out[usageKey(f.Region, f.Name)]
			u.UsageKnown, u.Invocations30d, u.LastInvoked, u.Daily = true, sums[i], last[i], daily[i]
		}
	}
}

// usageLogGroups reads retention and stored bytes: one paginated listing of
// /aws/lambda/ covers the default groups; a function with a custom LoggingConfig
// group gets its own lookup (bounded).
func usageLogGroups(ctx context.Context, api logGroupsAPI, fns []Function, out map[string]*FunctionUsage, notes *noteSet) {
	type info struct {
		retention int32
		stored    int64
	}
	listPrefix := func(prefix string) (map[string]info, error) {
		groups := map[string]info{}
		p := cloudwatchlogs.NewDescribeLogGroupsPaginator(api, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String(prefix)})
		for p.HasMorePages() {
			cctx, cancel := context.WithTimeout(ctx, activityPageTimeout)
			page, err := p.NextPage(cctx)
			cancel()
			if err != nil {
				return nil, err
			}
			for _, g := range page.LogGroups {
				groups[aws.ToString(g.LogGroupName)] = info{retention: aws.ToInt32(g.RetentionInDays), stored: aws.ToInt64(g.StoredBytes)}
			}
		}
		return groups, nil
	}
	apply := func(f Function, groups map[string]info) {
		u := out[usageKey(f.Region, f.Name)]
		u.LogKnown = true
		if g, ok := groups[f.LogGroup]; ok {
			u.LogExists, u.RetentionDays, u.StoredBytes = true, g.retention, g.stored
		}
	}

	var custom []Function
	var defaults []Function
	for _, f := range fns {
		if strings.HasPrefix(f.LogGroup, "/aws/lambda/") {
			defaults = append(defaults, f)
		} else {
			custom = append(custom, f)
		}
	}
	if len(defaults) > 0 {
		groups, err := listPrefix("/aws/lambda/")
		if err != nil {
			notes.add("log groups (logs:DescribeLogGroups)", err)
		} else {
			for _, f := range defaults {
				apply(f, groups)
			}
		}
	}
	forEachBounded(custom, func(f Function) {
		groups, err := listPrefix(f.LogGroup)
		if err != nil {
			notes.add("custom log groups (logs:DescribeLogGroups)", err)
			return
		}
		apply(f, groups)
	})
}

// usagePosture reads each function's resource policy and function URLs.
func usagePosture(ctx context.Context, api postureAPI, fns []Function, out map[string]*FunctionUsage, notes *noteSet) {
	forEachBounded(fns, func(f Function) {
		u := out[usageKey(f.Region, f.Name)]
		name := aws.String(f.Name)

		cctx, cancel := context.WithTimeout(ctx, drillTimeout)
		pol, err := api.GetPolicy(cctx, &lambda.GetPolicyInput{FunctionName: name})
		cancel()
		switch {
		case err == nil:
			u.PolicyKnown, u.Policy = true, aws.ToString(pol.Policy)
		case apiErrorCodeIs(err, "ResourceNotFoundException"):
			u.PolicyKnown = true // no policy
		default:
			notes.add("resource policies (lambda:GetPolicy)", err)
		}

		var auths []string
		p := lambda.NewListFunctionUrlConfigsPaginator(api, &lambda.ListFunctionUrlConfigsInput{FunctionName: name})
		for p.HasMorePages() {
			cctx, cancel := context.WithTimeout(ctx, drillTimeout)
			page, err := p.NextPage(cctx)
			cancel()
			if err != nil {
				notes.add("function URLs (lambda:ListFunctionUrlConfigs)", err)
				return
			}
			for _, c := range page.FunctionUrlConfigs {
				auths = append(auths, string(c.AuthType))
			}
		}
		u.URLKnown, u.URLAuthTypes = true, auths
	})
}

// forEachBounded runs f over items with at most usageWorkers in flight. Each
// call writes only its own function's entry, so no locking is needed.
func forEachBounded(items []Function, f func(Function)) {
	sem := make(chan struct{}, usageWorkers)
	var wg sync.WaitGroup
	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(it Function) {
			defer func() { <-sem; wg.Done() }()
			f(it)
		}(it)
	}
	wg.Wait()
}
