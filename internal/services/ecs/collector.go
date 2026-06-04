package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
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

	clusterPaginator := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list ECS clusters: %w", err)
		}

		if len(page.ClusterArns) == 0 {
			continue
		}

		desc, err := client.DescribeClusters(ctx, &ecs.DescribeClustersInput{
			Clusters: page.ClusterArns,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to describe ECS clusters: %w", err)
		}

		for _, cluster := range desc.Clusters {
			resources = append(resources, c.mapCluster(input.Region, cluster, input.DetailLevel))
		}
	}

	return resources, nil
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
		State:   state,
		Summary: map[string]string{
			"runningTasks":   fmt.Sprintf("%d", cluster.RunningTasksCount),
			"pendingTasks":   fmt.Sprintf("%d", cluster.PendingTasksCount),
			"activeServices": fmt.Sprintf("%d", cluster.ActiveServicesCount),
		},
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]interface{}{
			"registeredContainerInstances": cluster.RegisteredContainerInstancesCount,
		}
	}

	return res
}
