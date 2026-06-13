package ecs

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "ecs"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := ecs.NewFromConfig(input.AWSConfig)
	var resources []model.Resource
	var errs []error

	clusterPaginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list ECS clusters: %w", err)
		}

		if len(page.ClusterArns) == 0 {
			continue
		}

		desc, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: page.ClusterArns,
		})
		if err != nil {
			return resources, fmt.Errorf("failed to describe ECS clusters: %w", err)
		}

		// DescribeClusters returns per-cluster failures alongside the clusters
		// it could describe; record them so dropped clusters surface as a
		// partial result instead of vanishing silently.
		for _, f := range desc.Failures {
			errs = append(errs, fmt.Errorf("ECS cluster %s: %s", aws.ToString(f.Arn), aws.ToString(f.Reason)))
		}

		batch := make([]model.Resource, 0, len(desc.Clusters))
		for _, cluster := range desc.Clusters {
			batch = append(batch, c.mapCluster(input.Region, cluster, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, errors.Join(errs...)
}

func (c *Collector) mapCluster(region string, cluster types.Cluster, detail services.DetailLevel) model.Resource {
	id := aws.ToString(cluster.ClusterArn)
	name := aws.ToString(cluster.ClusterName)
	state := aws.ToString(cluster.Status)

	res := model.Resource{
		Service: "ecs",
		Type:    "cluster",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id, // ClusterArn is used as the resource ID
		State:   state,
		Summary: map[string]string{
			"runningTasks":   fmt.Sprintf("%d", cluster.RunningTasksCount),
			"pendingTasks":   fmt.Sprintf("%d", cluster.PendingTasksCount),
			"activeServices": fmt.Sprintf("%d", cluster.ActiveServicesCount),
		},
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"registeredContainerInstances": cluster.RegisteredContainerInstancesCount,
		}
	}

	return res
}
