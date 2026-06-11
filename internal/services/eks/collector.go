package eks

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// describeConcurrency bounds parallel DescribeCluster calls so accounts with
// many clusters don't serialize on per-cluster round-trips.
const describeConcurrency = 8

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "eks"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := eks.NewFromConfig(input.AWSConfig)

	// Describe each list page's clusters concurrently before fetching the
	// next page, so memory stays bounded to a page and results can stream
	// out page by page. Indexed writes keep list order. A failed describe
	// drops only that cluster, not the whole region.
	describePage := func(clusterNames []string) ([]model.Resource, []error) {
		described := make([]*model.Resource, len(clusterNames))
		var mu sync.Mutex
		var describeErrs []error
		var g errgroup.Group
		g.SetLimit(describeConcurrency)
		for i, clusterName := range clusterNames {
			g.Go(func() error {
				desc, err := client.DescribeCluster(ctx, &eks.DescribeClusterInput{
					Name: aws.String(clusterName),
				})
				if err != nil {
					mu.Lock()
					describeErrs = append(describeErrs, fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err))
					mu.Unlock()
					return nil
				}
				res := c.mapCluster(input.Region, desc.Cluster, input.DetailLevel)
				described[i] = &res
				return nil
			})
		}
		_ = g.Wait()

		batch := make([]model.Resource, 0, len(described))
		for _, r := range described {
			if r != nil {
				batch = append(batch, *r)
			}
		}
		return batch, describeErrs
	}

	var resources []model.Resource
	var errs []error
	paginator := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			// Keep everything described from earlier pages.
			errs = append(errs, fmt.Errorf("failed to list EKS clusters: %w", err))
			break
		}
		batch, describeErrs := describePage(page.Clusters)
		errs = append(errs, describeErrs...)
		resources = input.EmitOrAppend(resources, batch)
	}
	return resources, errors.Join(errs...)
}

func (c *Collector) mapCluster(region string, cluster *types.Cluster, detail services.DetailLevel) model.Resource {
	id := aws.ToString(cluster.Arn)
	name := aws.ToString(cluster.Name)
	state := string(cluster.Status)

	res := model.Resource{
		Service: "eks",
		Type:    "cluster",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id,
		State:   state,
		Summary: map[string]string{
			"version":  aws.ToString(cluster.Version),
			"endpoint": aws.ToString(cluster.Endpoint),
		},
	}

	if cluster.CreatedAt != nil {
		res.CreatedAt = cluster.CreatedAt
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"roleArn":         aws.ToString(cluster.RoleArn),
			"platformVersion": aws.ToString(cluster.PlatformVersion),
		}
	}

	return res
}
