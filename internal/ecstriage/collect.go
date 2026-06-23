package ecstriage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Collection follows the established best-effort pattern: each region is
// scanned independently, a failure empties that region (reported, never
// fatal), and the rest of the report proceeds.

// describeBatchSize is the DescribeTasks maximum (100 tasks per call).
const describeBatchSize = 100

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// Collect gathers recently stopped ECS tasks across the given regions and
// classifies them. When clusterFilter is non-empty only that cluster (by name
// or ARN) is scanned; otherwise every cluster in each region is scanned.
func Collect(ctx context.Context, baseCfg aws.Config, regions []string, clusterFilter string, maxConcurrency int, perCallTimeout time.Duration) ([]Record, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	type regionResult struct {
		recs []Record
		errs []model.ExploreError
	}
	results := make([]regionResult, len(regions))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			recs, errs := collectRegion(gctx, baseCfg, region, clusterFilter, perCallTimeout)
			results[i] = regionResult{recs: recs, errs: errs}
			return nil
		})
	}
	_ = g.Wait()

	var recs []Record
	var errs []model.ExploreError
	for _, r := range results {
		recs = append(recs, r.recs...)
		errs = append(errs, r.errs...)
	}
	Sort(recs)
	return recs, errs
}

func collectRegion(ctx context.Context, baseCfg aws.Config, region, clusterFilter string, timeout time.Duration) ([]Record, []model.ExploreError) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	cfg := baseCfg
	cfg.Region = region
	client := awsecs.NewFromConfig(cfg)

	clusters := []string{}
	if clusterFilter != "" {
		clusters = append(clusters, clusterFilter)
	} else {
		pager := awsecs.NewListClustersPaginator(client, &awsecs.ListClustersInput{})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, []model.ExploreError{exploreError(region, "ecs", err)}
			}
			clusters = append(clusters, page.ClusterArns...)
		}
	}

	var tasks []Task
	var errs []model.ExploreError
	for _, cluster := range clusters {
		ct, err := collectCluster(ctx, client, region, cluster)
		// Keep whatever was collected even on a partial error: a DescribeTasks
		// Failures entry drops one task, not the whole cluster (CLAUDE.md §6).
		tasks = append(tasks, ct...)
		if err != nil {
			errs = append(errs, exploreError(region, "ecs", err))
		}
	}
	return Classify(tasks), errs
}

func collectCluster(ctx context.Context, client *awsecs.Client, region, cluster string) ([]Task, error) {
	var arns []string
	pager := awsecs.NewListTasksPaginator(client, &awsecs.ListTasksInput{
		Cluster:       aws.String(cluster),
		DesiredStatus: types.DesiredStatusStopped,
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.TaskArns...)
	}
	if len(arns) == 0 {
		return nil, nil
	}

	clusterName := shortClusterName(cluster)
	var tasks []Task
	var describeErrs []error
	for start := 0; start < len(arns); start += describeBatchSize {
		end := start + describeBatchSize
		if end > len(arns) {
			end = len(arns)
		}
		out, err := client.DescribeTasks(ctx, &awsecs.DescribeTasksInput{
			Cluster: aws.String(cluster),
			Tasks:   arns[start:end],
		})
		if err != nil {
			return tasks, err
		}
		for _, t := range out.Tasks {
			tasks = append(tasks, mapTask(region, clusterName, t))
		}
		// Tasks that fail to describe come back in Failures, not Tasks — surface
		// them as a partial error so they aren't silently dropped (CLAUDE.md §6).
		for _, f := range out.Failures {
			describeErrs = append(describeErrs, fmt.Errorf("describe task %s: %s", aws.ToString(f.Arn), aws.ToString(f.Reason)))
		}
	}
	return tasks, errors.Join(describeErrs...)
}

func mapTask(region, clusterName string, t types.Task) Task {
	task := Task{
		ARN:           aws.ToString(t.TaskArn),
		Cluster:       clusterName,
		Region:        region,
		Group:         aws.ToString(t.Group),
		StopCode:      string(t.StopCode),
		StoppedReason: aws.ToString(t.StoppedReason),
		StoppedAt:     aws.ToTime(t.StoppedAt),
	}
	for _, c := range t.Containers {
		task.Containers = append(task.Containers, Container{
			Name:     aws.ToString(c.Name),
			ExitCode: c.ExitCode,
			Reason:   aws.ToString(c.Reason),
		})
	}
	return task
}

// shortClusterName reduces a cluster ARN to its name. Plain names pass through.
func shortClusterName(cluster string) string {
	if i := lastSlash(cluster); i >= 0 {
		return cluster[i+1:]
	}
	return cluster
}

func lastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return -1
}

func exploreError(region, service string, err error) model.ExploreError {
	code, msg := awserr.Classify(err, service, "")
	return model.ExploreError{Service: service, Region: region, Code: code, Message: msg}
}
