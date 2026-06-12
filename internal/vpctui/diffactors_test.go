package vpctui

import (
	"reflect"
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/trail"
)

func TestDiffActorTargets_UniqueInOrder(t *testing.T) {
	changes := []snapshotChange{
		{Kind: changeModified, Type: "Security group", ID: "sg-1"},
		{Kind: changeAdded, Type: "Route table", ID: "rtb-1"},
		{Kind: changeRemoved, Type: "Security group", ID: "sg-1"}, // dup
		{Kind: changeAdded, Type: "Subnet", ID: ""},               // no ID
	}
	targets, skipped := diffActorTargets(changes, 15)
	if !reflect.DeepEqual(targets, []string{"sg-1", "rtb-1"}) {
		t.Errorf("targets = %v", targets)
	}
	if skipped != 0 {
		t.Errorf("skipped = %d, want 0", skipped)
	}
}

func TestDiffActorTargets_Cap(t *testing.T) {
	var changes []snapshotChange
	for _, id := range []string{"a", "b", "c", "d"} {
		changes = append(changes, snapshotChange{ID: id})
	}
	targets, skipped := diffActorTargets(changes, 2)
	if !reflect.DeepEqual(targets, []string{"a", "b"}) {
		t.Errorf("targets = %v", targets)
	}
	if skipped != 2 {
		t.Errorf("skipped = %d, want 2", skipped)
	}
}

func TestFormatActor(t *testing.T) {
	ev := trail.Event{
		Time:      time.Date(2026, 6, 11, 14, 2, 0, 0, time.UTC),
		EventName: "AuthorizeSecurityGroupIngress",
		Principal: "role/deploy-pipeline",
	}
	got := formatActor(ev)
	want := "by role/deploy-pipeline — AuthorizeSecurityGroupIngress, 2026-06-11 14:02 UTC"
	if got != want {
		t.Errorf("formatActor = %q, want %q", got, want)
	}
}
