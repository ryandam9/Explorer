package services_test

import (
	"context"
	"testing"

	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
)

// stubCollector is a minimal Collector for testing.
type stubCollector struct{ name string }

func (s *stubCollector) Name() string    { return s.name }
func (s *stubCollector) IsGlobal() bool  { return false }
func (s *stubCollector) Collect(_ context.Context, _ services.CollectInput) ([]model.Resource, error) {
	return nil, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := services.NewRegistry()
	c := &stubCollector{name: "ec2"}
	r.Register(c)

	got, ok := r.Get("ec2")
	if !ok {
		t.Fatal("expected to find collector 'ec2'")
	}
	if got.Name() != "ec2" {
		t.Fatalf("got name %q, want %q", got.Name(), "ec2")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	r := services.NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for unregistered collector")
	}
}

func TestRegistry_GetAll_Sorted(t *testing.T) {
	r := services.NewRegistry()
	// Register in reverse alphabetical order to verify sort.
	for _, name := range []string{"s3", "ec2", "iam", "lambda"} {
		r.Register(&stubCollector{name: name})
	}

	all := r.GetAll()
	if len(all) != 4 {
		t.Fatalf("expected 4 collectors, got %d", len(all))
	}

	want := []string{"ec2", "iam", "lambda", "s3"}
	for i, c := range all {
		if c.Name() != want[i] {
			t.Fatalf("all[%d].Name() = %q, want %q", i, c.Name(), want[i])
		}
	}
}

func TestRegistry_GetAll_Empty(t *testing.T) {
	r := services.NewRegistry()
	if got := r.GetAll(); len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestRegistry_OverwriteCollector(t *testing.T) {
	r := services.NewRegistry()
	r.Register(&stubCollector{name: "ec2"})
	r.Register(&stubCollector{name: "ec2"}) // overwrite

	if len(r.GetAll()) != 1 {
		t.Fatal("duplicate registration should overwrite, not add")
	}
}
