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

	// Emit, when non-nil, receives page-sized batches of resources as soon as
	// they are gathered, so callers can stream partial results instead of
	// waiting for the whole collection to finish. Batches handed to Emit must
	// NOT also be returned from Collect. Collectors call Emit from a single
	// goroutine; a nil Emit means "accumulate and return" (the pre-streaming
	// contract), which collectors support via EmitOrAppend.
	Emit func(batch []model.Resource)
}

// EmitOrAppend streams batch through in.Emit when streaming is enabled,
// otherwise appends it to acc. Collectors use it once per page so the same
// code path serves both the streaming engine and direct Collect callers.
func (in CollectInput) EmitOrAppend(acc, batch []model.Resource) []model.Resource {
	if len(batch) == 0 {
		return acc
	}
	if in.Emit != nil {
		in.Emit(batch)
		return acc
	}
	return append(acc, batch...)
}

// Collector is the interface that every AWS service collector must implement.
//
// Collect is best-effort: when it fails partway through (a later page throttles,
// a per-item describe is denied, …) it returns the resources gathered so far
// together with the error, instead of discarding them. Callers must therefore
// consume the resource slice even when the error is non-nil.
type Collector interface {
	Name() string
	IsGlobal() bool
	Collect(ctx context.Context, input CollectInput) ([]model.Resource, error)
}
