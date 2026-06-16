package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/smithy-go"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
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

// ── StreamRun page-level streaming behaviour ─────────────────────────────────

// streamingCollector emits page batches through input.Emit (when streaming is
// enabled) and then returns the residual resources and error, mimicking a
// paginating collector.
type streamingCollector struct {
	name     string
	pages    [][]model.Resource
	residual []model.Resource
	err      error
}

func (s *streamingCollector) Name() string   { return s.name }
func (s *streamingCollector) IsGlobal() bool { return true }
func (s *streamingCollector) Collect(_ context.Context, input services.CollectInput) ([]model.Resource, error) {
	var acc []model.Resource
	for _, p := range s.pages {
		acc = input.EmitOrAppend(acc, p)
	}
	return append(acc, s.residual...), s.err
}

func TestStreamRun_StreamsOneChunkPerEmittedPage(t *testing.T) {
	eng := newFakeEngine(&streamingCollector{
		name: "fake",
		pages: [][]model.Resource{
			{{ID: "p1a"}, {ID: "p1b"}},
			{{ID: "p2a"}},
		},
	})

	chunks := make(chan model.ResultChunk, 16)
	go eng.StreamRun(context.Background(), chunks)
	var got []model.ResultChunk
	for c := range chunks {
		got = append(got, c)
	}

	// Two page chunks plus the final per-task chunk carrying the Progress
	// marker (sent even when no residual resources remain).
	if len(got) != 3 {
		t.Fatalf("expected 2 page chunks + 1 progress chunk, got %d: %+v", len(got), got)
	}
	if len(got[0].Resources) != 2 || got[0].Resources[0].ID != "p1a" {
		t.Errorf("unexpected first page: %+v", got[0].Resources)
	}
	if len(got[1].Resources) != 1 || got[1].Resources[0].ID != "p2a" {
		t.Errorf("unexpected second page: %+v", got[1].Resources)
	}
	last := got[2]
	if len(last.Resources) != 0 || last.Progress == nil {
		t.Fatalf("expected a final resources-free progress chunk, got %+v", last)
	}
	if last.Progress.Service != "fake" || last.Progress.Region != "global" {
		t.Errorf("unexpected progress origin: %+v", last.Progress)
	}
}

func TestStreamRun_ProgressEmittedForEveryTask(t *testing.T) {
	// A collector that returns nothing must still produce a progress marker,
	// or the TUI's done/total counter would never reach the total.
	eng := newFakeEngine(&fakeCollector{name: "fake"})

	chunks := make(chan model.ResultChunk, 16)
	go eng.StreamRun(context.Background(), chunks)
	progress := 0
	for c := range chunks {
		if c.Progress != nil {
			progress++
		}
	}
	if progress != 1 {
		t.Fatalf("expected exactly 1 progress marker, got %d", progress)
	}
}

func TestPlannedTaskKeys(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{name: "fake"}) // global collector
	keys := eng.PlannedTaskKeys()
	if len(keys) != 1 || keys[0] != "fake@global" {
		t.Fatalf("PlannedTaskKeys = %v, want [fake@global]", keys)
	}
}

func TestStreamRun_EmittedPagesAreFiltered(t *testing.T) {
	eng := newFakeEngine(&streamingCollector{
		name: "fake",
		pages: [][]model.Resource{
			{{ID: "r1", State: "running"}, {ID: "r2", State: "stopped"}},
			{{ID: "r3", State: "stopped"}},
		},
	})
	eng.Config.Filters.States = []string{"running"}

	resources, errs := collectChunks(eng)

	if len(resources) != 1 || resources[0].ID != "r1" {
		t.Fatalf("expected only the running resource to be streamed, got %v", resources)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestStreamRun_PartialFlagSetWhenPagesWereStreamedBeforeError(t *testing.T) {
	eng := newFakeEngine(&streamingCollector{
		name:  "fake",
		pages: [][]model.Resource{{{ID: "p1"}}},
		err:   errors.New("throttled after first page"),
	})

	resources, errs := collectChunks(eng)

	if len(resources) != 1 || resources[0].ID != "p1" {
		t.Fatalf("expected the streamed page to be kept, got %v", resources)
	}
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !errs[0].Partial {
		t.Error("expected Partial=true when pages were streamed before the failure")
	}
}

func TestStreamRun_DeadlineExceededTaggedAsTimeout(t *testing.T) {
	eng := newFakeEngine(&fakeCollector{
		name: "fake",
		err:  fmt.Errorf("operation failed: %w", context.DeadlineExceeded),
	})
	eng.Config.App.TimeoutSeconds = 30

	_, errs := collectChunks(eng)

	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if errs[0].Code != "Timeout" {
		t.Errorf("Code = %q, want Timeout", errs[0].Code)
	}
	if !strings.Contains(errs[0].Message, "30s") {
		t.Errorf("timeout message should mention the configured deadline, got %q", errs[0].Message)
	}
}

func TestRegionScopeLabel(t *testing.T) {
	tests := []struct {
		name       string
		regions    []string
		allRegions bool
		want       string
	}{
		{"single region", []string{"ap-southeast-2"}, false, "ap-southeast-2"},
		{"multiple regions", []string{"us-east-1", "eu-west-1"}, false, "us-east-1,eu-west-1"},
		{"all regions flag", []string{"us-east-1"}, true, "all"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := regionScopeLabel(tt.regions, tt.allRegions); got != tt.want {
				t.Errorf("regionScopeLabel(%v, %v) = %q, want %q", tt.regions, tt.allRegions, got, tt.want)
			}
		})
	}
}

// regionalFake is a non-global collector so an account can have multiple tasks
// (one per region), letting the "report failure once" behavior be observed.
type regionalFake struct{ name string }

func (f *regionalFake) Name() string   { return f.name }
func (f *regionalFake) IsGlobal() bool { return false }
func (f *regionalFake) Collect(context.Context, services.CollectInput) ([]model.Resource, error) {
	return nil, nil
}

func TestStreamRun_AccountConfigFailureSurfacedAndProgressCompletes(t *testing.T) {
	orig := buildAccountConfig
	buildAccountConfig = func(context.Context, *config.AWSConfig, string) (aws.Config, error) {
		return aws.Config{}, errors.New("SharedConfigProfileNotExistError: profile \"bogus\" not found")
	}
	t.Cleanup(func() { buildAccountConfig = orig })

	eng := newFakeEngine(&regionalFake{name: "fake"})
	eng.ResolvedRegions = []string{"us-east-1", "eu-west-1"} // 2 tasks for the account
	eng.Config.Accounts = []config.AccountConfig{{Name: "acctA", Profile: "bogus"}}

	chunks := make(chan model.ResultChunk, 16)
	go eng.StreamRun(context.Background(), chunks)
	var resources []model.Resource
	var errs []model.ExploreError
	progress := 0
	for c := range chunks {
		resources = append(resources, c.Resources...)
		errs = append(errs, c.Errors...)
		if c.Progress != nil {
			progress++
		}
	}

	if len(resources) != 0 {
		t.Fatalf("a failed account collects nothing, got %d resources", len(resources))
	}
	// Progress must reach every planned task (2 regions) so the meter completes.
	if want := len(eng.PlannedTaskKeys()); progress != want {
		t.Fatalf("progress markers = %d, want %d (one per planned task)", progress, want)
	}
	// The identical failure is reported exactly once, not once per task.
	if len(errs) != 1 {
		t.Fatalf("expected the account failure reported once, got %d: %v", len(errs), errs)
	}
	if errs[0].Code != "AccountCredentialsError" {
		t.Errorf("Code = %q, want AccountCredentialsError", errs[0].Code)
	}
	if !strings.Contains(errs[0].Message, "acctA") {
		t.Errorf("message should name the skipped account, got %q", errs[0].Message)
	}
}
