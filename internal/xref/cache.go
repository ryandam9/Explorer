package xref

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/model"
)

// The related/whereused scan is broad and re-runs from scratch every
// invocation (and on every jump from the summary TUI). A short-lived on-disk
// cache lets repeated exploration of the same account/region scope reuse a
// recent scan instead of re-hitting AWS (#393). It is opt-in (a zero TTL
// disables it) and best-effort: any cache error falls back to a live scan.

// CacheEntry is a persisted scan: the collected edges plus the best-effort
// collection errors, stamped so staleness and version skew can be detected.
type CacheEntry struct {
	Version   string               `json:"version"`
	CreatedAt time.Time            `json:"created_at"`
	Edges     []Edge               `json:"edges"`
	Errors    []model.ExploreError `json:"errors,omitempty"`
}

// CacheKey is a stable filename stem for a scan scope. Region order doesn't
// change the scope, so it's normalized; version is included so an upgraded
// binary (possibly emitting new edge types) never reads a stale-shaped cache.
func CacheKey(version, accountID, profile string, regions []string) string {
	rs := append([]string(nil), regions...)
	sort.Strings(rs)
	parts := append([]string{version, accountID, profile}, rs...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

// CachePath is the on-disk location for a cache key, under the user cache dir.
func CachePath(key string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "aws_explorer", "related", key+".json"), nil
}

// LoadCache returns the cached entry when caching is enabled (ttl > 0), the
// file exists and parses, and it's within ttl of now. Any other condition
// (including a version mismatch) is a miss — never an error.
func LoadCache(path string, version string, ttl time.Duration, now time.Time) (CacheEntry, bool) {
	if ttl <= 0 || path == "" {
		return CacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheEntry{}, false
	}
	var e CacheEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return CacheEntry{}, false
	}
	if e.Version != version {
		return CacheEntry{}, false
	}
	if now.Sub(e.CreatedAt) > ttl {
		return CacheEntry{}, false
	}
	return e, true
}

// SaveCache writes an entry, creating the cache directory as needed.
func SaveCache(path string, e CacheEntry) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
