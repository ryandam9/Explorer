package vpctui

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// snapshotTTL is how long a fetched VPC snapshot stays fresh. Within the TTL,
// opening another overlay (findings → trace → xref → effective rules →
// exposure → export) reuses the snapshot instead of re-issuing the nine List
// calls. The refresh key invalidates it and the snapshot diff bypasses it, so
// anything that must observe live state still does.
const snapshotTTL = 30 * time.Second

type snapshotEntry struct {
	snap      vpcSnapshot
	fetchedAt time.Time
}

// snapshotCache memoizes VPC snapshots per VPC ID for a short TTL. Overlay
// loaders run in their own goroutines (tea.Cmd), so access is mutex-guarded
// and concurrent fetches of the same VPC are collapsed into one flight.
type snapshotCache struct {
	mu      sync.Mutex
	entries map[string]snapshotEntry
	group   singleflight.Group
	ttl     time.Duration
	now     func() time.Time // injectable for tests
}

func newSnapshotCache() *snapshotCache {
	return &snapshotCache{
		entries: make(map[string]snapshotEntry),
		ttl:     snapshotTTL,
		now:     time.Now,
	}
}

// get returns the cached snapshot for vpcID while it is fresh, calling fetch
// (and caching its result) otherwise. Errors are returned, never cached.
func (sc *snapshotCache) get(vpcID string, fetch func() (vpcSnapshot, error)) (vpcSnapshot, error) {
	if snap, ok := sc.lookup(vpcID); ok {
		return snap, nil
	}
	v, err, _ := sc.group.Do(vpcID, func() (any, error) {
		// Re-check: an earlier flight may have stored it while we waited.
		if snap, ok := sc.lookup(vpcID); ok {
			return snap, nil
		}
		return sc.fetchAndStore(vpcID, fetch)
	})
	if err != nil {
		return vpcSnapshot{}, err
	}
	return v.(vpcSnapshot), nil
}

// refresh always fetches a live snapshot and stores it, so callers that must
// see current state (the snapshot diff) also warm the cache for everyone else.
func (sc *snapshotCache) refresh(vpcID string, fetch func() (vpcSnapshot, error)) (vpcSnapshot, error) {
	v, err := sc.fetchAndStore(vpcID, fetch)
	if err != nil {
		return vpcSnapshot{}, err
	}
	return v.(vpcSnapshot), nil
}

// invalidate forgets the cached snapshot for vpcID (refresh key, VPC re-entry).
func (sc *snapshotCache) invalidate(vpcID string) {
	sc.mu.Lock()
	delete(sc.entries, vpcID)
	sc.mu.Unlock()
}

func (sc *snapshotCache) lookup(vpcID string) (vpcSnapshot, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	e, ok := sc.entries[vpcID]
	if !ok || sc.now().Sub(e.fetchedAt) >= sc.ttl {
		return vpcSnapshot{}, false
	}
	return e.snap, true
}

func (sc *snapshotCache) fetchAndStore(vpcID string, fetch func() (vpcSnapshot, error)) (any, error) {
	snap, err := fetch()
	if err != nil {
		return vpcSnapshot{}, err
	}
	sc.mu.Lock()
	sc.entries[vpcID] = snapshotEntry{snap: snap, fetchedAt: sc.now()}
	sc.mu.Unlock()
	return snap, nil
}
