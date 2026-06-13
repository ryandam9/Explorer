package rds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// Collector implements the services.Collector interface for RDS.
type Collector struct{}

// NewCollector returns a new RDS Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "rds"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := rds.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	// 1. Collect DB Instances
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe RDS instances: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.DBInstances))
		for _, instance := range page.DBInstances {
			batch = append(batch, c.mapInstance(input.Region, instance, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	// 2. Collect DB Clusters (Aurora / Serverless v2). A cluster with no
	// provisioned instances is invisible in the instance listing above, so it
	// must be collected separately.
	clusterPaginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})
	for clusterPaginator.HasMorePages() {
		page, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe RDS clusters: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.DBClusters))
		for _, cluster := range page.DBClusters {
			batch = append(batch, c.mapCluster(input.Region, cluster, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapCluster(region string, cluster types.DBCluster, detail services.DetailLevel) model.Resource {
	id := aws.ToString(cluster.DBClusterIdentifier)

	var tags map[string]string
	if len(cluster.TagList) > 0 {
		tags = make(map[string]string, len(cluster.TagList))
		for _, t := range cluster.TagList {
			tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
		}
	}

	res := model.Resource{
		Service: "rds",
		Type:    "cluster",
		Region:  region,
		ID:      id,
		Name:    id,
		ARN:     aws.ToString(cluster.DBClusterArn),
		State:   aws.ToString(cluster.Status),
		Tags:    tags,
		Summary: map[string]string{
			"engine":        aws.ToString(cluster.Engine),
			"engineVersion": aws.ToString(cluster.EngineVersion),
			"engineMode":    aws.ToString(cluster.EngineMode),
		},
	}

	if cluster.ClusterCreateTime != nil {
		res.CreatedAt = cluster.ClusterCreateTime
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"multiAZ":     aws.ToBool(cluster.MultiAZ),
			"memberCount": len(cluster.DBClusterMembers),
		}
	}

	return res
}

func (c *Collector) mapInstance(region string, instance types.DBInstance, detail services.DetailLevel) model.Resource {
	id := aws.ToString(instance.DBInstanceIdentifier)
	state := aws.ToString(instance.DBInstanceStatus)
	iClass := aws.ToString(instance.DBInstanceClass)
	engine := aws.ToString(instance.Engine)

	// DescribeDBInstances already returns the instance's tags inline (TagList),
	// so populate them here without an extra ListTagsForResource call.
	var tags map[string]string
	if len(instance.TagList) > 0 {
		tags = make(map[string]string, len(instance.TagList))
		for _, t := range instance.TagList {
			tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
		}
	}

	res := model.Resource{
		Service: "rds",
		Type:    "instance",
		Region:  region,
		AZ:      aws.ToString(instance.AvailabilityZone),
		ID:      id,
		Name:    id,
		ARN:     aws.ToString(instance.DBInstanceArn),
		State:   state,
		Tags:    tags,
		Summary: map[string]string{
			"instanceClass": iClass,
			"engine":        engine,
			"engineVersion": aws.ToString(instance.EngineVersion),
		},
	}

	if instance.InstanceCreateTime != nil {
		res.CreatedAt = instance.InstanceCreateTime
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"allocatedStorage":   aws.ToInt32(instance.AllocatedStorage),
			"multiAZ":            aws.ToBool(instance.MultiAZ),
			"publiclyAccessible": aws.ToBool(instance.PubliclyAccessible),
		}
	}

	return res
}
