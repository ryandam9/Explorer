package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/aws/smithy-go"

	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

func TestFilterResources_NoFilters(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", State: "running"},
		{ID: "r2", State: "stopped"},
	}
	got := filterResources(resources, model.Filter{})
	// Early-return: must be the exact same slice
	if &got[0] != &resources[0] {
		t.Fatal("expected same slice to be returned when no filters are set")
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(got))
	}
}

func TestFilterResources_ByState(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", State: "running"},
		{ID: "r2", State: "stopped"},
		{ID: "r3", State: "Running"}, // case-insensitive
	}
	got := filterResources(resources, model.Filter{States: []string{"running"}})
	if len(got) != 2 {
		t.Fatalf("expected 2 resources, got %d: %v", len(got), got)
	}
	for _, r := range got {
		if r.ID == "r2" {
			t.Fatal("stopped resource should have been filtered out")
		}
	}
}

func TestFilterResources_ByTag_Match(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", Tags: map[string]string{"env": "prod", "team": "platform"}},
		{ID: "r2", Tags: map[string]string{"env": "dev"}},
		{ID: "r3", Tags: nil},
	}
	got := filterResources(resources, model.Filter{Tags: map[string]string{"env": "prod"}})
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("expected only r1, got %v", got)
	}
}

func TestFilterResources_ByTagMultiple_AllMustMatch(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", Tags: map[string]string{"env": "prod", "team": "platform"}},
		{ID: "r2", Tags: map[string]string{"env": "prod"}}, // missing "team"
	}
	got := filterResources(resources, model.Filter{Tags: map[string]string{"env": "prod", "team": "platform"}})
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("expected only r1, got %v", got)
	}
}

func TestFilterResources_ByStateAndTag(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", State: "running", Tags: map[string]string{"env": "prod"}},
		{ID: "r2", State: "running", Tags: map[string]string{"env": "dev"}},
		{ID: "r3", State: "stopped", Tags: map[string]string{"env": "prod"}},
	}
	got := filterResources(resources, model.Filter{
		States: []string{"running"},
		Tags:   map[string]string{"env": "prod"},
	})
	if len(got) != 1 || got[0].ID != "r1" {
		t.Fatalf("expected only r1, got %v", got)
	}
}

func TestFilterResources_EmptyInput(t *testing.T) {
	got := filterResources(nil, model.Filter{States: []string{"running"}})
	if len(got) != 0 {
		t.Fatalf("expected empty result for nil input, got %v", got)
	}
}

func TestFilterResources_NoMatches(t *testing.T) {
	resources := []model.Resource{
		{ID: "r1", State: "stopped"},
	}
	got := filterResources(resources, model.Filter{States: []string{"running"}})
	if len(got) != 0 {
		t.Fatalf("expected empty result, got %v", got)
	}
}

// ── StreamRun partial-results behaviour ──────────────────────────────────────

type fakeCollector struct {
	name      string
	resources []model.Resource
	err       error
}

func (f *fakeCollector) Name() string   { return f.name }
func (f *fakeCollector) IsGlobal() bool { return true }
func (f *fakeCollector) Collect(_ context.Context, _ services.CollectInput) ([]model.Resource, error) {
	return f.resources, f.err
}

// newFakeEngine builds an Engine wired to a single enabled fake collector.
func newFakeEngine(c services.Collector) *Engine {
	registry := services.NewRegistry()
	registry.Register(c)
	return &Engine{
		Config: &config.Config{
			Services: map[string]config.ServiceConfig{
				c.Name(): {Enabled: true},
			},
		},
		registry:        registry,
		ResolvedRegions: []string{"us-east-1"},
	}
}

func collectChunks(e *Engine) (resources []model.Resource, errs []model.ExploreError) {
	chunks := make(chan model.ResultChunk, 16)
	go e.StreamRun(context.Background(), chunks)
	for chunk := range chunks {
		resources = append(resources, chunk.Resources...)
		errs = append(errs, chunk.Errors...)
	}
	return resources, errs
}

func TestStreamRun_PartialResultsKeptOnError(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name:      "fake",
		resources: []model.Resource{{ID: "r1"}, {ID: "r2"}},
		err:       errors.New("throttled: rate exceeded"),
	})

	resources, errs := collectChunks(eng)

	if len(resources) != 2 {
		t.Fatalf("expected the 2 partial resources to be kept, got %d", len(resources))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	e := errs[0]
	if !e.Partial {
		t.Error("expected the error to be flagged Partial when resources were kept")
	}
	if e.Code != "CollectionError" {
		t.Errorf("Code = %q, want CollectionError", e.Code)
	}
	if e.Service != "fake" || e.Region != "global" {
		t.Errorf("unexpected error origin: %+v", e)
	}
}

func TestStreamRun_ErrorWithoutResourcesNotPartial(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name: "fake",
		err:  errors.New("boom"),
	})

	resources, errs := collectChunks(eng)

	if len(resources) != 0 {
		t.Fatalf("expected no resources, got %d", len(resources))
	}
	if len(errs) != 1 || errs[0].Partial {
		t.Fatalf("expected a single non-partial error, got %v", errs)
	}
}

func TestStreamRun_PartialResultsAreFiltered(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name: "fake",
		resources: []model.Resource{
			{ID: "r1", State: "running"},
			{ID: "r2", State: "stopped"},
		},
		err: errors.New("throttled"),
	})
	eng.Config.Filters.States = []string{"running"}

	resources, errs := collectChunks(eng)

	if len(resources) != 1 || resources[0].ID != "r1" {
		t.Fatalf("expected only the running resource to survive filtering, got %v", resources)
	}
	if len(errs) != 1 || !errs[0].Partial {
		t.Fatalf("expected a partial error, got %v", errs)
	}
}

func TestStreamRun_PartialFlagFalseWhenAllFilteredOut(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name:      "fake",
		resources: []model.Resource{{ID: "r1", State: "stopped"}},
		err:       errors.New("throttled"),
	})
	eng.Config.Filters.States = []string{"running"}

	resources, errs := collectChunks(eng)

	if len(resources) != 0 {
		t.Fatalf("expected no resources after filtering, got %v", resources)
	}
	if len(errs) != 1 || errs[0].Partial {
		t.Fatalf("expected a non-partial error when nothing was kept, got %v", errs)
	}
}

func TestStreamRun_AccessDeniedPartialKeepsResources(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name:      "fake",
		resources: []model.Resource{{ID: "r1"}},
		err: fmt.Errorf("page 2 failed: %w", &smithy.GenericAPIError{
			Code:    "AccessDeniedException",
			Message: "User is not authorized to perform: fake:ListThings on resource: *",
		}),
	})

	resources, errs := collectChunks(eng)

	if len(resources) != 1 {
		t.Fatalf("expected the partial resource to be kept, got %d", len(resources))
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if errs[0].Code != "AccessDenied" || !errs[0].Partial {
		t.Errorf("expected a partial AccessDenied error, got %+v", errs[0])
	}
}
