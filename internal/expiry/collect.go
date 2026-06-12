package expiry

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsacm "github.com/aws/aws-sdk-go-v2/service/acm"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	awssecrets "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Collection is best-effort in the established pattern: every service family
// is fetched independently, a failure empties that family (its checks then
// produce no items) and is reported, and the rest of the report proceeds.

// recorder accumulates collection errors for one region.
type recorder struct {
	region string
	errs   []model.ExploreError
}

func (r *recorder) record(service string, err error) {
	if err == nil {
		return
	}
	code, msg := awserr.Classify(err, service, "")
	r.errs = append(r.errs, model.ExploreError{
		Service: service, Region: r.region, Code: code, Message: msg,
	})
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// Collect gathers every deadline across the given regions (plus the global
// IAM server certificates) and returns them sorted soonest-first.
func Collect(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]Item, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}
	now := time.Now().UTC()

	type regionResult struct {
		items []Item
		errs  []model.ExploreError
	}
	results := make([]regionResult, len(regions)+1)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			items, errs := collectRegion(gctx, baseCfg, region, now, perCallTimeout)
			results[i] = regionResult{items: items, errs: errs}
			return nil
		})
	}
	// IAM server certificates are global; collect them once.
	g.Go(func() error {
		items, errs := collectIAMServerCerts(gctx, baseCfg, now, perCallTimeout)
		results[len(regions)] = regionResult{items: items, errs: errs}
		return nil
	})
	_ = g.Wait()

	var items []Item
	var errs []model.ExploreError
	for _, r := range results {
		items = append(items, r.items...)
		errs = append(errs, r.errs...)
	}
	Sort(items)
	return items, errs
}

func collectRegion(ctx context.Context, baseCfg aws.Config, region string, now time.Time, timeout time.Duration) ([]Item, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region
	rec := &recorder{region: region}

	var items []Item
	items = append(items, CertItems(now, region, fetchACMCerts(ctx, cfg, rec, timeout))...)
	items = append(items, LambdaItems(now, region, fetchFunctions(ctx, cfg, rec, timeout))...)
	items = append(items, EKSItems(now, region, fetchClusters(ctx, cfg, rec, timeout))...)
	dbs, maint := fetchRDS(ctx, cfg, rec, timeout)
	items = append(items, RDSItems(now, region, dbs, maint)...)
	items = append(items, SecretItems(now, region, fetchSecrets(ctx, cfg, rec, timeout))...)
	return items, rec.errs
}

func fetchACMCerts(ctx context.Context, cfg aws.Config, rec *recorder, timeout time.Duration) []Certificate {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsacm.NewFromConfig(cfg)

	var certs []Certificate
	pager := awsacm.NewListCertificatesPaginator(client, &awsacm.ListCertificatesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("acm", err)
			break
		}
		for _, c := range page.CertificateSummaryList {
			certs = append(certs, Certificate{
				Name:     aws.ToString(c.DomainName),
				ARN:      aws.ToString(c.CertificateArn),
				NotAfter: aws.ToTime(c.NotAfter),
				InUse:    aws.ToBool(c.InUse),
				Source:   "acm",
			})
		}
	}
	return certs
}

func collectIAMServerCerts(ctx context.Context, cfg aws.Config, now time.Time, timeout time.Duration) ([]Item, []model.ExploreError) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	rec := &recorder{region: "global"}
	client := awsiam.NewFromConfig(cfg)

	var certs []Certificate
	pager := awsiam.NewListServerCertificatesPaginator(client, &awsiam.ListServerCertificatesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			break
		}
		for _, c := range page.ServerCertificateMetadataList {
			certs = append(certs, Certificate{
				Name:     aws.ToString(c.ServerCertificateName),
				ARN:      aws.ToString(c.Arn),
				NotAfter: aws.ToTime(c.Expiration),
				Source:   "iam",
			})
		}
	}
	return CertItems(now, "global", certs), rec.errs
}

func fetchFunctions(ctx context.Context, cfg aws.Config, rec *recorder, timeout time.Duration) []Function {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awslambda.NewFromConfig(cfg)

	var fns []Function
	pager := awslambda.NewListFunctionsPaginator(client, &awslambda.ListFunctionsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("lambda", err)
			break
		}
		for _, f := range page.Functions {
			fns = append(fns, Function{
				Name:    aws.ToString(f.FunctionName),
				Runtime: string(f.Runtime),
			})
		}
	}
	return fns
}

func fetchClusters(ctx context.Context, cfg aws.Config, rec *recorder, timeout time.Duration) []Cluster {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awseks.NewFromConfig(cfg)

	var names []string
	pager := awseks.NewListClustersPaginator(client, &awseks.ListClustersInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("eks", err)
			return nil
		}
		names = append(names, page.Clusters...)
	}

	var clusters []Cluster
	errReported := false
	for _, name := range names {
		out, err := client.DescribeCluster(ctx, &awseks.DescribeClusterInput{Name: aws.String(name)})
		if err != nil {
			if !errReported {
				rec.record("eks", err)
				errReported = true
			}
			continue
		}
		if out.Cluster != nil {
			clusters = append(clusters, Cluster{
				Name:    name,
				Version: aws.ToString(out.Cluster.Version),
			})
		}
	}
	return clusters
}

func fetchRDS(ctx context.Context, cfg aws.Config, rec *recorder, timeout time.Duration) ([]DBInstance, []Maintenance) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsrds.NewFromConfig(cfg)

	var dbs []DBInstance
	dbPager := awsrds.NewDescribeDBInstancesPaginator(client, &awsrds.DescribeDBInstancesInput{})
	for dbPager.HasMorePages() {
		page, err := dbPager.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, db := range page.DBInstances {
			dbs = append(dbs, DBInstance{
				ID:       aws.ToString(db.DBInstanceIdentifier),
				CACertID: aws.ToString(db.CACertificateIdentifier),
			})
		}
	}

	var maint []Maintenance
	mPager := awsrds.NewDescribePendingMaintenanceActionsPaginator(client, &awsrds.DescribePendingMaintenanceActionsInput{})
	for mPager.HasMorePages() {
		page, err := mPager.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, res := range page.PendingMaintenanceActions {
			id := shortResourceID(aws.ToString(res.ResourceIdentifier))
			for _, a := range res.PendingMaintenanceActionDetails {
				maint = append(maint, Maintenance{
					Resource:         id,
					Action:           aws.ToString(a.Action),
					Description:      aws.ToString(a.Description),
					AutoAppliedAfter: aws.ToTime(a.AutoAppliedAfterDate),
					ForcedApply:      aws.ToTime(a.ForcedApplyDate),
					CurrentApply:     aws.ToTime(a.CurrentApplyDate),
				})
			}
		}
	}
	return dbs, maint
}

func fetchSecrets(ctx context.Context, cfg aws.Config, rec *recorder, timeout time.Duration) []Secret {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awssecrets.NewFromConfig(cfg)

	var secrets []Secret
	pager := awssecrets.NewListSecretsPaginator(client, &awssecrets.ListSecretsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("secretsmanager", err)
			break
		}
		for _, s := range page.SecretList {
			sec := Secret{
				Name:            aws.ToString(s.Name),
				RotationEnabled: aws.ToBool(s.RotationEnabled),
				NextRotation:    aws.ToTime(s.NextRotationDate),
				LastRotated:     aws.ToTime(s.LastRotatedDate),
			}
			if s.RotationRules != nil {
				sec.RotateAfterDays = aws.ToInt64(s.RotationRules.AutomaticallyAfterDays)
			}
			secrets = append(secrets, sec)
		}
	}
	return secrets
}

// shortResourceID trims an RDS resource ARN down to its identifier tail.
func shortResourceID(arn string) string {
	if i := strings.LastIndexByte(arn, ':'); i >= 0 && i+1 < len(arn) {
		return arn[i+1:]
	}
	return arn
}
