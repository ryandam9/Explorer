package engine

import (
	"testing"

	"github.com/user/aws_explorer/internal/model"
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
