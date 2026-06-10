package eks

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"golang.org/x/sync/errgroup"

	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
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

	var clusterNames []string
	paginator := eks.NewListClustersPaginator(client, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list EKS clusters: %w", err)
		}
		clusterNames = append(clusterNames, page.Clusters...)
	}

	// Describe clusters concurrently; indexed writes keep list order.
	resources := make([]model.Resource, len(clusterNames))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(describeConcurrency)
	for i, clusterName := range clusterNames {
		g.Go(func() error {
			desc, err := client.DescribeCluster(gctx, &eks.DescribeClusterInput{
				Name: aws.String(clusterName),
			})
			if err != nil {
				return fmt.Errorf("failed to describe EKS cluster %s: %w", clusterName, err)
			}
			resources[i] = c.mapCluster(input.Region, desc.Cluster, input.DetailLevel)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	return resources, nil
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
