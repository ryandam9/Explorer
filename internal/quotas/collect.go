package quotas

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscw "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	awssq "github.com/aws/aws-sdk-go-v2/service/servicequotas"
	sqtypes "github.com/aws/aws-sdk-go-v2/service/servicequotas/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// quotaRef names one curated quota by its stable Service Quotas codes. These
// are the limits that actually cause incidents; the display name is taken from
// the API response (authoritative) so this list only needs the codes.
type quotaRef struct {
	service string
	code    string
	global  bool // account-level (queried once, e.g. IAM), not per-region
}

// registry is the curated ~20 quotas. Codes are AWS's documented, stable
// Service Quotas quota codes. An unknown/removed code degrades to a skipped
// quota (best-effort), never a crash.
var registry = []quotaRef{
	{service: "ec2", code: "L-1216C47A"},                  // Running On-Demand Standard (A,C,D,H,I,M,R,T,Z) instances — vCPUs
	{service: "ec2", code: "L-34B43A08"},                  // Running On-Demand G and VT instances — vCPUs
	{service: "ec2", code: "L-0263D0A3"},                  // EC2-VPC Elastic IPs
	{service: "vpc", code: "L-F678F1CE"},                  // VPCs per Region
	{service: "vpc", code: "L-DF5E4CA3"},                  // Network interfaces per Region
	{service: "vpc", code: "L-A4707A72"},                  // Internet gateways per Region
	{service: "vpc", code: "L-FE5A380F"},                  // NAT gateways per Availability Zone
	{service: "vpc", code: "L-E79EC296"},                  // VPC security groups per Region
	{service: "lambda", code: "L-B99A9384"},               // Concurrent executions
	{service: "rds", code: "L-7B6409FD"},                  // DB instances
	{service: "rds", code: "L-7ADDB58A"},                  // Total storage for all DB instances
	{service: "ebs", code: "L-D18FCD1D"},                  // Storage for General Purpose SSD (gp3) volumes
	{service: "ebs", code: "L-309BACF6"},                  // Storage for General Purpose SSD (gp2) volumes
	{service: "elasticloadbalancing", code: "L-53DA6B97"}, // Application Load Balancers per Region
	{service: "elasticloadbalancing", code: "L-69A177A2"}, // Network Load Balancers per Region
	{service: "eks", code: "L-1194D53C"},                  // Clusters
	{service: "iam", code: "L-FE177D64", global: true},    // Roles per account
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

func classifyErr(region, service string, err error) model.ExploreError {
	code, msg := awserr.Classify(err, service, "")
	return model.ExploreError{Service: service, Region: region, Code: code, Message: msg}
}

// Collect fetches the curated quotas (per-region for regional services, once
// for global ones) across the given regions and returns them with any
// collection errors. Each quota is independent: a failed lookup is reported
// and skipped, never fatal.
func Collect(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]Quota, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	var regional, global []quotaRef
	for _, r := range registry {
		if r.global {
			global = append(global, r)
		} else {
			regional = append(regional, r)
		}
	}

	type result struct {
		quotas []Quota
		errs   []model.ExploreError
	}
	results := make([]result, len(regions)+1)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			q, errs := collectQuotas(gctx, baseCfg, region, regional, perCallTimeout)
			results[i] = result{quotas: q, errs: errs}
			return nil
		})
	}
	// Global quotas live in us-east-1 for IAM; collect them once there.
	g.Go(func() error {
		q, errs := collectQuotas(gctx, baseCfg, "us-east-1", global, perCallTimeout)
		for i := range q {
			q[i].Region = "global"
		}
		results[len(regions)] = result{quotas: q, errs: errs}
		return nil
	})
	_ = g.Wait()

	var quotas []Quota
	var errs []model.ExploreError
	for _, r := range results {
		quotas = append(quotas, r.quotas...)
		errs = append(errs, r.errs...)
	}
	return quotas, errs
}

func collectQuotas(ctx context.Context, baseCfg aws.Config, region string, refs []quotaRef, timeout time.Duration) ([]Quota, []model.ExploreError) {
	if len(refs) == 0 {
		return nil, nil
	}
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	cfg := baseCfg
	cfg.Region = region
	sqClient := awssq.NewFromConfig(cfg)
	cwClient := awscw.NewFromConfig(cfg)

	var quotas []Quota
	var errs []model.ExploreError
	var pending []pendingUsage
	for _, ref := range refs {
		sq, fromDefault, err := getQuota(ctx, sqClient, ref)
		if err != nil {
			errs = append(errs, classifyErr(region, "servicequotas", err))
			continue
		}
		q := Quota{
			Name:        aws.ToString(sq.QuotaName),
			Service:     ref.service,
			Region:      region,
			Limit:       aws.ToFloat64(sq.Value),
			Unit:        normalizeUnit(aws.ToString(sq.Unit)),
			FromDefault: fromDefault,
		}
		if m := sq.UsageMetric; m != nil && aws.ToString(m.MetricName) != "" && aws.ToString(m.MetricNamespace) != "" {
			pending = append(pending, pendingUsage{idx: len(quotas), metric: m})
		}
		quotas = append(quotas, q)
	}
	// Fetch every quota's usage in one batched GetMetricData sweep rather than
	// a GetMetricStatistics round-trip per quota.
	fetchUsageBatch(ctx, cwClient, quotas, pending)
	return quotas, errs
}

// pendingUsage links a resolved quota (by index into the quotas slice) to the
// CloudWatch usage metric whose value still needs fetching.
type pendingUsage struct {
	idx    int
	metric *sqtypes.MetricInfo
}

// getQuota returns the applied quota value, falling back to the AWS default
// (so a never-adjusted quota still reports its limit). The bool is true when
// the default was used.
func getQuota(ctx context.Context, client *awssq.Client, ref quotaRef) (*sqtypes.ServiceQuota, bool, error) {
	out, err := client.GetServiceQuota(ctx, &awssq.GetServiceQuotaInput{
		ServiceCode: aws.String(ref.service),
		QuotaCode:   aws.String(ref.code),
	})
	if err == nil && out.Quota != nil && out.Quota.Value != nil {
		return out.Quota, false, nil
	}
	// No applied value (common — most quotas are never adjusted): use the
	// AWS default, which still reflects the real ceiling.
	def, derr := client.GetAWSDefaultServiceQuota(ctx, &awssq.GetAWSDefaultServiceQuotaInput{
		ServiceCode: aws.String(ref.service),
		QuotaCode:   aws.String(ref.code),
	})
	if derr != nil {
		if err != nil {
			return nil, false, err // surface the original applied-value error
		}
		return nil, false, derr
	}
	return def.Quota, true, nil
}

// usageLookback is how far back fetchUsage searches for a usage datapoint.
// Wide enough to catch metrics that publish only hourly or daily; the most
// recent datapoint in the window is the one used.
const usageLookback = 48 * time.Hour

// maxMetricQueries is the GetMetricData limit on MetricDataQueries per call.
const maxMetricQueries = 500

// fetchUsageBatch fills Used/UsageKnown for the pending quotas with one batched
// GetMetricData sweep (chunked at the API's query limit), instead of a separate
// GetMetricStatistics call per quota. Best-effort: on error the affected
// quotas are simply left with usage unknown. Each query uses the quota's
// AWS-recommended statistic; GetMetricData computes it server-side, so the
// returned Values are the statistic directly (newest first via a descending
// scan), and the first value seen for an id is the most recent.
func fetchUsageBatch(ctx context.Context, client *awscw.Client, quotas []Quota, pending []pendingUsage) {
	if len(pending) == 0 {
		return
	}
	queries := make([]cwtypes.MetricDataQuery, 0, len(pending))
	for n, p := range pending {
		m := p.metric
		stat := aws.ToString(m.MetricStatisticRecommendation)
		if stat == "" {
			stat = "Maximum"
		}
		dims := make([]cwtypes.Dimension, 0, len(m.MetricDimensions))
		for k, v := range m.MetricDimensions {
			dims = append(dims, cwtypes.Dimension{Name: aws.String(k), Value: aws.String(v)})
		}
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(fmt.Sprintf("u%d", n)),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  m.MetricNamespace,
					MetricName: m.MetricName,
					Dimensions: dims,
				},
				Period: aws.Int32(300),
				Stat:   aws.String(stat),
			},
		})
	}

	end := time.Now()
	start := end.Add(-usageLookback)
	latest := make(map[string]float64, len(queries))
	for offset := 0; offset < len(queries); offset += maxMetricQueries {
		chunk := queries[offset:min(offset+maxMetricQueries, len(queries))]
		pager := awscw.NewGetMetricDataPaginator(client, &awscw.GetMetricDataInput{
			MetricDataQueries: chunk,
			StartTime:         aws.Time(start),
			EndTime:           aws.Time(end),
			ScanBy:            cwtypes.ScanByTimestampDescending,
		})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return // best-effort: leave usage unknown for this region
			}
			for _, r := range page.MetricDataResults {
				id := aws.ToString(r.Id)
				if _, seen := latest[id]; seen || len(r.Values) == 0 {
					continue // keep the first (most-recent) value for an id
				}
				latest[id] = r.Values[0]
			}
		}
	}

	for n, p := range pending {
		if v, ok := latest[fmt.Sprintf("u%d", n)]; ok {
			quotas[p.idx].Used = v
			quotas[p.idx].UsageKnown = true
		}
	}
}

// normalizeUnit hides Service Quotas' "None" placeholder unit.
func normalizeUnit(u string) string {
	if u == "" || u == "None" {
		return ""
	}
	return u
}
