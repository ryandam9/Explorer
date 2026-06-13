package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
)

// costMetricWindow is the traffic/consumption lookback for the metric-based
// checks (idle load balancers, over-provisioned DynamoDB tables).
const costMetricWindow = 14 * 24 * time.Hour

// metricPeriodSeconds: daily datapoints keep result payloads small; sums are
// aggregated client-side over the window.
const metricPeriodSeconds = 86400

// ddbMetricPeriodSeconds samples DynamoDB consumption hourly rather than
// daily, so the busiest hour stands out as a peak. Provisioned capacity must
// cover that peak, so the over-provisioning estimate sizes from it; a daily
// period would smear a short spike into a low daily average.
const ddbMetricPeriodSeconds = 3600

// maxQueriesPerCall is the GetMetricData limit on MetricDataQueries.
const maxQueriesPerCall = 500

// fetchCostMetrics populates the metric-derived snapshot fields (load
// balancer traffic, DynamoDB consumption) with one batched GetMetricData
// sweep. On failure the fields stay nil and the dependent checks skip; the
// error is reported so the skip is visible.
func fetchCostMetrics(ctx context.Context, cfg aws.Config, snap *findings.CostSnapshot, rec *errRecorder, timeout time.Duration) {
	queries, bind := buildCostMetricQueries(snap)
	if len(queries) == 0 {
		return
	}

	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awscloudwatch.NewFromConfig(cfg)

	end := snap.Now
	start := end.Add(-costMetricWindow)

	sums := make(map[string]float64, len(queries))
	maxes := make(map[string]float64, len(queries))
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
				return // leave every metric field nil: skip, don't guess
			}
			for _, r := range page.MetricDataResults {
				id := aws.ToString(r.Id)
				for _, v := range r.Values {
					sums[id] += v
					if v > maxes[id] {
						maxes[id] = v
					}
				}
			}
		}
	}
	bind(sums, maxes)
}

// buildCostMetricQueries assembles the GetMetricData queries for the
// snapshot's load balancers and provisioned tables, returning the queries and
// a bind function that writes the resolved sums back into the snapshot.
// CloudWatch never returns datapoints for periods with no activity, so an ID
// missing from the results means zero — which is exactly the signal the idle
// checks look for; only a failed call leaves the fields nil.
func buildCostMetricQueries(snap *findings.CostSnapshot) ([]cwtypes.MetricDataQuery, func(map[string]float64, map[string]float64)) {
	var queries []cwtypes.MetricDataQuery
	type binding struct {
		id    string
		apply func(sum, max float64)
	}
	var bindings []binding

	addQuery := func(id, namespace, metric, dimName, dimValue string, period int32, apply func(sum, max float64)) {
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String(namespace),
					MetricName: aws.String(metric),
					Dimensions: []cwtypes.Dimension{{
						Name:  aws.String(dimName),
						Value: aws.String(dimValue),
					}},
				},
				Period: aws.Int32(period),
				Stat:   aws.String("Sum"),
			},
		})
		bindings = append(bindings, binding{id: id, apply: apply})
	}

	for i := range snap.LoadBalancers {
		lb := &snap.LoadBalancers[i]
		dim, ok := lbMetricDimension(lb.ARN)
		if !ok {
			continue
		}
		id := fmt.Sprintf("lb%d", i)
		switch lb.Type {
		case "application":
			addQuery(id, "AWS/ApplicationELB", "RequestCount", "LoadBalancer", dim, metricPeriodSeconds,
				func(sum, _ float64) { lb.Requests14d = &sum })
		case "network":
			addQuery(id, "AWS/NetworkELB", "NewFlowCount", "LoadBalancer", dim, metricPeriodSeconds,
				func(sum, _ float64) { lb.Requests14d = &sum })
			// Gateway load balancers have no request-style metric worth an
			// idle verdict; they are skipped.
		}
	}

	windowSeconds := costMetricWindow.Seconds()
	for i := range snap.Tables {
		t := &snap.Tables[i]
		if t.ProvisionedRCU+t.ProvisionedWCU == 0 {
			continue // on-demand tables have nothing to over-provision
		}
		// avg = total consumed over the window / window seconds; peak = the
		// busiest hour's consumption / hour seconds (a per-second rate
		// comparable to provisioned units).
		addQuery(fmt.Sprintf("tr%d", i), "AWS/DynamoDB", "ConsumedReadCapacityUnits", "TableName", t.Name, ddbMetricPeriodSeconds,
			func(sum, max float64) {
				avg := sum / windowSeconds
				peak := max / ddbMetricPeriodSeconds
				t.AvgConsumedRCU, t.PeakConsumedRCU = &avg, &peak
			})
		addQuery(fmt.Sprintf("tw%d", i), "AWS/DynamoDB", "ConsumedWriteCapacityUnits", "TableName", t.Name, ddbMetricPeriodSeconds,
			func(sum, max float64) {
				avg := sum / windowSeconds
				peak := max / ddbMetricPeriodSeconds
				t.AvgConsumedWCU, t.PeakConsumedWCU = &avg, &peak
			})
	}

	bind := func(sums, maxes map[string]float64) {
		for _, b := range bindings {
			b.apply(sums[b.id], maxes[b.id]) // missing ID = 0 datapoints = zero activity
		}
	}
	return queries, bind
}

// lbMetricDimension derives the CloudWatch LoadBalancer dimension value from
// an ELBv2 ARN: everything after ":loadbalancer/" (e.g. "app/my-alb/abc123").
func lbMetricDimension(arn string) (string, bool) {
	const marker = ":loadbalancer/"
	i := strings.Index(arn, marker)
	if i < 0 {
		return "", false
	}
	dim := arn[i+len(marker):]
	if dim == "" {
		return "", false
	}
	return dim, true
}
