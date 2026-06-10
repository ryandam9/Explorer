package services

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/model"
)

// DetailLevel represents the granularity of collected data.
type DetailLevel string

const (
	DetailLevelMinimal  DetailLevel = "minimal"
	DetailLevelSummary  DetailLevel = "summary"
	DetailLevelDetailed DetailLevel = "detailed"
	DetailLevelRaw      DetailLevel = "raw"
)

// CollectInput represents the input for a collector.
type CollectInput struct {
	Config      *config.Config
	AWSConfig   aws.Config
	Region      string
	AccountID   string
	Filters     model.Filter
	DetailLevel DetailLevel
}

// Collector is the interface that every AWS service collector must implement.
type Collector interface {
	Name() string
	IsGlobal() bool
	Collect(ctx context.Context, input CollectInput) ([]model.Resource, error)
}
