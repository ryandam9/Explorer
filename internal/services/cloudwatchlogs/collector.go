package cloudwatchlogs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"

	"github.com/user/aws_explorer/internal/logs"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

// Collector implements the services.Collector interface for CloudWatch Logs
// log groups.
type Collector struct{}

// NewCollector returns a new CloudWatch Logs Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "cloudwatchlogs"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := cloudwatchlogs.NewFromConfig(input.AWSConfig)

	groups, err := logs.ListGroups(ctx, client, input.Region)
	resources := make([]model.Resource, 0, len(groups))
	for _, g := range groups {
		resources = append(resources, c.mapGroup(g))
	}
	return resources, err
}

func (c *Collector) mapGroup(g logs.Group) model.Resource {
	res := model.Resource{
		Service: "cloudwatchlogs",
		Type:    "log_group",
		Region:  g.Region,
		ID:      g.Name,
		Name:    g.Name,
		ARN:     g.ARN,
		Summary: map[string]string{
			"retention":   logs.FormatRetention(g.RetentionDays),
			"storedBytes": fmt.Sprintf("%d", g.StoredBytes),
			"storedSize":  logs.FormatBytes(g.StoredBytes),
		},
	}
	if !g.CreatedAt.IsZero() {
		created := g.CreatedAt
		res.CreatedAt = &created
	}
	return res
}
