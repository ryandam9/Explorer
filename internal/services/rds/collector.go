package rds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
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
			return nil, fmt.Errorf("failed to describe RDS instances: %w", err)
		}

		for _, instance := range page.DBInstances {
			resources = append(resources, c.mapInstance(input.Region, instance, input.DetailLevel))
		}
	}

	return resources, nil
}

func (c *Collector) mapInstance(region string, instance types.DBInstance, detail services.DetailLevel) model.Resource {
	id := aws.ToString(instance.DBInstanceIdentifier)
	state := aws.ToString(instance.DBInstanceStatus)
	iClass := aws.ToString(instance.DBInstanceClass)
	engine := aws.ToString(instance.Engine)

	res := model.Resource{
		Service: "rds",
		Type:    "instance",
		Region:  region,
		ID:      id,
		Name:    id,
		State:   state,
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
