package vpctui

import (
	"strings"
	"testing"
)

func diffBase() vpcSnapshot {
	return vpcSnapshot{
		VPCID: "vpc-1",
		SecurityGroups: []SGInfo{
			{ID: "sg-web", Rules: []SGRule{
				{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
			}},
			{ID: "sg-old", Rules: nil},
		},
		RouteTables: []RouteTableInfo{
			{ID: "rtb-1", Routes: []Route{{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"}}},
		},
		Subnets: []SubnetInfo{
			{ID: "subnet-1", CIDR: "10.0.0.0/24", AZ: "a", AvailableIPs: 200},
		},
	}
}

func changeFor(changes []snapshotChange, id string) *snapshotChange {
	for i := range changes {
		if changes[i].ID == id {
			return &changes[i]
		}
	}
	return nil
}

func TestDiffAddedRemovedModified(t *testing.T) {
	old := diffBase()
	neu := diffBase()
	// Add a new SG, remove sg-old, and add a rule to sg-web.
	neu.SecurityGroups = []SGInfo{
		{ID: "sg-web", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
			{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/8"}, // new
		}},
		{ID: "sg-new", Rules: nil},
	}

	changes := diffSnapshots(old, neu)

	if c := changeFor(changes, "sg-new"); c == nil || c.Kind != changeAdded {
		t.Errorf("expected sg-new added, got %+v", c)
	}
	if c := changeFor(changes, "sg-old"); c == nil || c.Kind != changeRemoved {
		t.Errorf("expected sg-old removed, got %+v", c)
	}
	c := changeFor(changes, "sg-web")
	if c == nil || c.Kind != changeModified {
		t.Fatalf("expected sg-web modified, got %+v", c)
	}
	if len(c.Added) != 1 || !strings.Contains(c.Added[0], "22") {
		t.Errorf("expected the added 22 rule, got %+v", c.Added)
	}
	if len(c.Removed) != 0 {
		t.Errorf("expected no removed rules, got %+v", c.Removed)
	}
}

func TestDiffNoChanges(t *testing.T) {
	// Available-IP count changes must NOT register as a diff (volatile field).
	old := diffBase()
	neu := diffBase()
	neu.Subnets[0].AvailableIPs = 5 // changed, but excluded from the fingerprint
	if changes := diffSnapshots(old, neu); len(changes) != 0 {
		t.Errorf("expected no changes, got %+v", changes)
	}
}

func TestDiffRouteChange(t *testing.T) {
	old := diffBase()
	neu := diffBase()
	neu.RouteTables[0].Routes = []Route{{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"}}
	c := changeFor(diffSnapshots(old, neu), "rtb-1")
	if c == nil || c.Kind != changeModified {
		t.Fatalf("expected rtb-1 modified, got %+v", c)
	}
	if len(c.Added) != 1 || !strings.Contains(c.Added[0], "nat-1") {
		t.Errorf("expected nat-1 route added, got %+v", c.Added)
	}
	if len(c.Removed) != 1 || !strings.Contains(c.Removed[0], "igw-1") {
		t.Errorf("expected igw-1 route removed, got %+v", c.Removed)
	}
}

func TestDiffCounts(t *testing.T) {
	changes := []snapshotChange{
		{Kind: changeAdded}, {Kind: changeAdded}, {Kind: changeRemoved}, {Kind: changeModified},
	}
	a, r, m := diffCounts(changes)
	if a != 2 || r != 1 || m != 1 {
		t.Errorf("counts = (%d,%d,%d), want (2,1,1)", a, r, m)
	}
}

func TestSaveLoadSnapshotRoundTrip(t *testing.T) {
	// Redirect HOME so the snapshot is written to a temp dir.
	t.Setenv("HOME", t.TempDir())

	snap := diffBase()
	if err := saveSnapshot(snap); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}
	got, ok, err := loadSnapshot("vpc-1", "")
	if err != nil || !ok {
		t.Fatalf("loadSnapshot: ok=%v err=%v", ok, err)
	}
	if got.VPCID != "vpc-1" || len(got.SecurityGroups) != 2 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	// A diff of the loaded baseline against itself should be empty.
	if changes := diffSnapshots(got, snap); len(changes) != 0 {
		t.Errorf("self-diff should be empty, got %+v", changes)
	}
}

func TestSaveLoadSnapshotAccountScoped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	snap := diffBase()
	snap.OwnerID = "111122223333"
	if err := saveSnapshot(snap); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}
	// Loading with the owning account finds the baseline; a different account
	// must not see it.
	if _, ok, err := loadSnapshot("vpc-1", "111122223333"); err != nil || !ok {
		t.Fatalf("owner-scoped load: ok=%v err=%v", ok, err)
	}
	if _, ok, _ := loadSnapshot("vpc-1", "444455556666"); ok {
		t.Error("baseline from another account should not be visible")
	}
}

func TestLoadSnapshotLegacyFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A baseline saved before account scoping (no OwnerID → vpc-1.json) must
	// still be found when loading with an owner.
	if err := saveSnapshot(diffBase()); err != nil {
		t.Fatalf("saveSnapshot: %v", err)
	}
	got, ok, err := loadSnapshot("vpc-1", "111122223333")
	if err != nil || !ok {
		t.Fatalf("legacy fallback load: ok=%v err=%v", ok, err)
	}
	if got.VPCID != "vpc-1" {
		t.Errorf("legacy fallback mismatch: %+v", got)
	}
}

func TestLoadSnapshotMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, ok, err := loadSnapshot("vpc-absent", "")
	if err != nil {
		t.Errorf("missing baseline should not error, got %v", err)
	}
	if ok {
		t.Error("expected ok=false for a missing baseline")
	}
}
