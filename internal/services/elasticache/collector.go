// Package elasticache collects ElastiCache cache clusters. A typed collector is
// needed because the Resource Groups Tagging API only returns tagged resources;
// an untagged cache cluster is invisible to the broad discovery sweep.
package elasticache

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "elasticache" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := elasticache.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var marker *string
	for {
		page, err := client.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{Marker: marker})
		if err != nil {
			return resources, fmt.Errorf("failed to describe ElastiCache clusters: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.CacheClusters))
		for _, cl := range page.CacheClusters {
			batch = append(batch, c.mapCluster(cl, input.Region))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.Marker == nil {
			break
		}
		marker = page.Marker
	}
	return resources, nil
}

func (c *Collector) mapCluster(cl types.CacheCluster, region string) model.Resource {
	id := aws.ToString(cl.CacheClusterId)
	res := model.Resource{
		Service: "elasticache",
		Type:    "cacheCluster",
		Region:  region,
		ID:      id,
		Name:    id,
		ARN:     aws.ToString(cl.ARN),
		State:   aws.ToString(cl.CacheClusterStatus),
		Summary: map[string]string{
			"engine":   aws.ToString(cl.Engine),
			"nodeType": aws.ToString(cl.CacheNodeType),
		},
	}
	return res
}
