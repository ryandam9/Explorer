package emr

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	"github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "emr"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := emr.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := emr.NewListClustersPaginator(client, &emr.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to list EMR clusters: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.Clusters))
		for _, cluster := range page.Clusters {
			batch = append(batch, c.mapCluster(input.Region, cluster, input.DetailLevel))
		}
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapCluster(region string, cluster types.ClusterSummary, detail services.DetailLevel) model.Resource {
	id := aws.ToString(cluster.Id)
	name := aws.ToString(cluster.Name)
	state := ""
	if cluster.Status != nil {
		state = string(cluster.Status.State)
	}

	res := model.Resource{
		Service: "emr",
		Type:    "cluster",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     aws.ToString(cluster.ClusterArn),
		State:   state,
		Summary: map[string]string{
			"normalizedInstanceHours": fmt.Sprintf("%d", aws.ToInt32(cluster.NormalizedInstanceHours)),
		},
	}

	if cluster.Status != nil && cluster.Status.Timeline != nil && cluster.Status.Timeline.CreationDateTime != nil {
		res.CreatedAt = cluster.Status.Timeline.CreationDateTime
	}

	return res
}
