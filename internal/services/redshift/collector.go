// Package redshift collects Redshift clusters. A typed collector is needed
// because the Resource Groups Tagging API only returns tagged resources; an
// untagged cluster is invisible to the broad discovery sweep.
package redshift

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/redshift/types"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector { return &Collector{} }

func (c *Collector) Name() string { return "redshift" }

func (c *Collector) IsGlobal() bool { return false }

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := redshift.NewFromConfig(input.AWSConfig)

	var resources []model.Resource
	var marker *string
	for {
		page, err := client.DescribeClusters(ctx, &redshift.DescribeClustersInput{Marker: marker})
		if err != nil {
			return resources, fmt.Errorf("failed to describe Redshift clusters: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Clusters))
		for _, cl := range page.Clusters {
			batch = append(batch, c.mapCluster(cl, input.Region, input.AccountID))
		}
		resources = input.EmitOrAppend(resources, batch)
		if page.Marker == nil {
			break
		}
		marker = page.Marker
	}
	return resources, nil
}

func (c *Collector) mapCluster(cl types.Cluster, region, account string) model.Resource {
	id := aws.ToString(cl.ClusterIdentifier)
	// DescribeClusters returns no cluster ARN, so construct it to match the form
	// the Tagging API emits, letting the two merge on the same ARN.
	arn := fmt.Sprintf("arn:aws:redshift:%s:%s:cluster:%s", region, account, id)
	return model.Resource{
		Service:   "redshift",
		Type:      "cluster",
		Region:    region,
		ID:        id,
		Name:      id,
		ARN:       arn,
		State:     aws.ToString(cl.ClusterStatus),
		CreatedAt: cl.ClusterCreateTime,
		Summary: map[string]string{
			"nodeType": aws.ToString(cl.NodeType),
		},
	}
}
