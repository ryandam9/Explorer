package emrtui

import (
	"strings"
	"testing"
	"time"

	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

// liveInstanceStates must be "every state except TERMINATED", derived from the
// SDK's own enum rather than written out and forgotten. If AWS adds a state,
// this fails instead of quietly dropping instances in it — the filter must
// never make a live node invisible (CLAUDE.md §2: "not queried" must not look
// like "doesn't exist").
func TestLiveInstanceStatesCoverEverythingButTerminated(t *testing.T) {
	live := map[emrtypes.InstanceState]bool{}
	for _, s := range liveInstanceStates {
		if live[s] {
			t.Errorf("duplicate state %q", s)
		}
		live[s] = true
	}
	if live[emrtypes.InstanceStateTerminated] {
		t.Error("TERMINATED must not be in the live set — excluding it is the point")
	}
	for _, s := range emrtypes.InstanceState("").Values() {
		if s == emrtypes.InstanceStateTerminated {
			continue
		}
		if !live[s] {
			t.Errorf("state %q is not terminated but is filtered out; add it to liveInstanceStates", s)
		}
	}
}

// An empty instances section has to say it is filtered. "None" after a filter
// reads as "this cluster has no nodes", which is a different claim.
func TestInstancesBodyExplainsTheFilter(t *testing.T) {
	body := instancesBody(nil)
	for _, want := range []string{"no live instances", "terminated"} {
		if !strings.Contains(strings.ToLower(body), want) {
			t.Errorf("empty body should mention %q, got %q", want, body)
		}
	}
}

// The section titles in both renderings say the list is the live one, so a
// count that disagrees with the console's instance history is explainable.
func TestInstancesSectionsSayLive(t *testing.T) {
	d := ClusterDescription{Cluster: Cluster{ID: "j-1", Region: "us-east-1"}}

	var found bool
	for _, s := range d.sections() {
		if s.Title == "EC2 instances (live)" {
			found = true
		}
	}
	if !found {
		t.Error("describe section is not labelled as the live instances")
	}
	if md := clusterMarkdown(d, time.Unix(0, 0).UTC()); !strings.Contains(md, "## EC2 instances (live)") {
		t.Errorf("markdown heading is not labelled as the live instances:\n%s", md)
	}
}
