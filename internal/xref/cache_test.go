package xref

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheKey_StableAndRegionOrderInsensitive(t *testing.T) {
	a := CacheKey("v1", "111", "default", []string{"us-east-1", "eu-west-1"})
	b := CacheKey("v1", "111", "default", []string{"eu-west-1", "us-east-1"})
	if a != b {
		t.Errorf("region order must not change the key: %s vs %s", a, b)
	}
	// Any scope dimension changing must change the key.
	for _, diff := range []string{
		CacheKey("v2", "111", "default", []string{"us-east-1", "eu-west-1"}),
		CacheKey("v1", "222", "default", []string{"us-east-1", "eu-west-1"}),
		CacheKey("v1", "111", "prod", []string{"us-east-1", "eu-west-1"}),
		CacheKey("v1", "111", "default", []string{"us-east-1"}),
	} {
		if diff == a {
			t.Errorf("key collision: %s", diff)
		}
	}
}

func TestCache_RoundTripAndTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.json")
	entry := CacheEntry{
		Version:   "v1",
		CreatedAt: time.Now(),
		Edges:     []Edge{{From: Reference{Service: "lambda", ID: "fn"}, Target: "role"}},
	}
	if err := SaveCache(path, entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Fresh and matching version → hit.
	got, ok := LoadCache(path, "v1", time.Minute, time.Now())
	if !ok || len(got.Edges) != 1 || got.Edges[0].Target != "role" {
		t.Errorf("expected a fresh hit, got ok=%v entry=%+v", ok, got)
	}

	// Past the TTL → miss.
	if _, ok := LoadCache(path, "v1", time.Minute, entry.CreatedAt.Add(2*time.Minute)); ok {
		t.Errorf("stale entry should miss")
	}
	// Version skew → miss.
	if _, ok := LoadCache(path, "v2", time.Minute, time.Now()); ok {
		t.Errorf("version mismatch should miss")
	}
	// TTL disabled → miss.
	if _, ok := LoadCache(path, "v1", 0, time.Now()); ok {
		t.Errorf("ttl<=0 should disable the cache")
	}
	// Missing file → miss, no error.
	if _, ok := LoadCache(filepath.Join(t.TempDir(), "nope.json"), "v1", time.Minute, time.Now()); ok {
		t.Errorf("missing file should miss")
	}
}
