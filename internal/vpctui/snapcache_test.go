package vpctui

import (
	"errors"
	"testing"
	"time"
)

func TestSnapshotCacheServesWithinTTLAndExpires(t *testing.T) {
	sc := newSnapshotCache()
	now := time.Now()
	sc.now = func() time.Time { return now }

	calls := 0
	fetch := func() (vpcSnapshot, error) {
		calls++
		return vpcSnapshot{VPCID: "vpc-1"}, nil
	}

	for range 3 {
		snap, err := sc.get("vpc-1", fetch)
		if err != nil {
			t.Fatal(err)
		}
		if snap.VPCID != "vpc-1" {
			t.Fatalf("unexpected snapshot: %+v", snap)
		}
	}
	if calls != 1 {
		t.Fatalf("expected a single fetch within the TTL, got %d", calls)
	}

	now = now.Add(snapshotTTL + time.Second)
	if _, err := sc.get("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected a re-fetch after the TTL elapsed, got %d calls", calls)
	}
}

func TestSnapshotCacheInvalidateForcesRefetch(t *testing.T) {
	sc := newSnapshotCache()
	calls := 0
	fetch := func() (vpcSnapshot, error) {
		calls++
		return vpcSnapshot{VPCID: "vpc-1"}, nil
	}

	if _, err := sc.get("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	sc.invalidate("vpc-1")
	if _, err := sc.get("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected invalidate to force a re-fetch, got %d calls", calls)
	}
}

func TestSnapshotCacheRefreshBypassesTTLAndStores(t *testing.T) {
	sc := newSnapshotCache()
	calls := 0
	fetch := func() (vpcSnapshot, error) {
		calls++
		return vpcSnapshot{VPCID: "vpc-1"}, nil
	}

	if _, err := sc.get("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	// refresh must hit AWS even though the entry is fresh…
	if _, err := sc.refresh("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected refresh to fetch live state, got %d calls", calls)
	}
	// …and re-warm the cache for subsequent get calls.
	if _, err := sc.get("vpc-1", fetch); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected get after refresh to be served from cache, got %d calls", calls)
	}
}

func TestSnapshotCacheDoesNotCacheErrors(t *testing.T) {
	sc := newSnapshotCache()
	calls := 0
	boom := errors.New("boom")
	fetch := func() (vpcSnapshot, error) {
		calls++
		if calls == 1 {
			return vpcSnapshot{}, boom
		}
		return vpcSnapshot{VPCID: "vpc-1"}, nil
	}

	if _, err := sc.get("vpc-1", fetch); !errors.Is(err, boom) {
		t.Fatalf("expected the fetch error to surface, got %v", err)
	}
	snap, err := sc.get("vpc-1", fetch)
	if err != nil || snap.VPCID != "vpc-1" {
		t.Fatalf("expected the retry to fetch successfully, got (%+v, %v)", snap, err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 fetches (error not cached), got %d", calls)
	}
}

func TestSnapshotCacheIsPerVPC(t *testing.T) {
	sc := newSnapshotCache()
	calls := map[string]int{}
	fetchFor := func(id string) func() (vpcSnapshot, error) {
		return func() (vpcSnapshot, error) {
			calls[id]++
			return vpcSnapshot{VPCID: id}, nil
		}
	}

	a, _ := sc.get("vpc-a", fetchFor("vpc-a"))
	b, _ := sc.get("vpc-b", fetchFor("vpc-b"))
	if a.VPCID != "vpc-a" || b.VPCID != "vpc-b" {
		t.Fatalf("snapshots crossed VPCs: %+v / %+v", a, b)
	}
	if calls["vpc-a"] != 1 || calls["vpc-b"] != 1 {
		t.Fatalf("expected one fetch per VPC, got %v", calls)
	}
}
