// Package acctsnap implements account-wide inventory snapshot diffing:
// baseline the whole merged-by-ARN inventory, then diff a later scan against
// it — "what changed in this account since yesterday?". It is the
// account-level twin of the VPC explorer's snapshot diff
// (internal/vpctui/snapshotdiff.go): the diff is a pure function over two
// snapshots, and volatile fields are excluded so an unchanged account
// produces an empty, deterministic diff.
package acctsnap

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/summary"
)

// Entry is one resource in a snapshot, reduced to the stable identity facts
// worth diffing. Volatile fields (metrics, free-form details, timestamps) are
// deliberately excluded — the lesson from the VPC diff.
type Entry struct {
	Key    string            `json:"key"` // ARN when present, else type|region|id
	ID     string            `json:"id,omitempty"`
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type"` // "service/type", e.g. "ec2/instance"
	Region string            `json:"region,omitempty"`
	State  string            `json:"state,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
}

// Snapshot is a point-in-time inventory of an account under a region scope.
type Snapshot struct {
	TakenAt   time.Time `json:"takenAt"`
	AccountID string    `json:"accountId"`
	Regions   []string  `json:"regions"` // sorted region scope of the scan
	Entries   []Entry   `json:"entries"` // sorted by Key
}

// New builds a snapshot from collected resources. Resources are deduplicated
// by ARN (richer entry wins, same rule as the summary inventory) and entries
// are sorted by key so snapshots — and therefore diffs — are deterministic.
func New(resources []model.Resource, accountID string, regions []string) Snapshot {
	deduped := summary.Dedupe(resources)
	entries := make([]Entry, 0, len(deduped))
	for _, r := range deduped {
		entries = append(entries, Entry{
			Key:    entryKey(r),
			ID:     r.ID,
			Name:   r.Name,
			Type:   typeLabel(r),
			Region: r.Region,
			State:  r.State,
			Tags:   r.Tags,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })

	scope := append([]string(nil), regions...)
	sort.Strings(scope)

	return Snapshot{
		TakenAt:   time.Now().UTC(),
		AccountID: accountID,
		Regions:   scope,
		Entries:   entries,
	}
}

// entryKey identifies a resource across scans: the ARN when present,
// otherwise a composite that cannot collide across types or regions.
func entryKey(r model.Resource) string {
	if r.ARN != "" {
		return r.ARN
	}
	return typeLabel(r) + "|" + r.Region + "|" + r.ID
}

func typeLabel(r model.Resource) string {
	switch {
	case r.Service != "" && r.Type != "":
		return r.Service + "/" + r.Type
	case r.Service != "":
		return r.Service
	default:
		return r.Type
	}
}

// Change kinds, in render order.
const (
	KindAdded    = "added"
	KindRemoved  = "removed"
	KindModified = "modified"
)

// Change is one difference between two snapshots.
type Change struct {
	Kind   string   `json:"kind"`
	Type   string   `json:"type"`
	Name   string   `json:"name,omitempty"`
	ID     string   `json:"id,omitempty"`
	Region string   `json:"region,omitempty"`
	Key    string   `json:"key"`
	Deltas []string `json:"deltas,omitempty"` // modified only: "state: stopped → running"
}

// Diff returns the ordered list of changes turning old into neu. Matched keys
// are compared on name, state and tags only; the result is sorted by type,
// kind and key so it is stable across runs.
func Diff(old, neu Snapshot) []Change {
	oldM := make(map[string]Entry, len(old.Entries))
	for _, e := range old.Entries {
		oldM[e.Key] = e
	}
	neuM := make(map[string]Entry, len(neu.Entries))
	for _, e := range neu.Entries {
		neuM[e.Key] = e
	}

	keys := make([]string, 0, len(oldM)+len(neuM))
	for k := range oldM {
		keys = append(keys, k)
	}
	for k := range neuM {
		if _, seen := oldM[k]; !seen {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var out []Change
	for _, k := range keys {
		o, inOld := oldM[k]
		n, inNew := neuM[k]
		switch {
		case inNew && !inOld:
			out = append(out, change(KindAdded, n, nil))
		case inOld && !inNew:
			out = append(out, change(KindRemoved, o, nil))
		default:
			if deltas := entryDeltas(o, n); len(deltas) > 0 {
				out = append(out, change(KindModified, n, deltas))
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func change(kind string, e Entry, deltas []string) Change {
	return Change{
		Kind: kind, Type: e.Type, Name: e.Name, ID: e.ID,
		Region: e.Region, Key: e.Key, Deltas: deltas,
	}
}

// entryDeltas lists the field-level differences between two matched entries
// as "field: old → new" strings, tags included key by key.
func entryDeltas(o, n Entry) []string {
	var out []string
	if o.Name != n.Name {
		out = append(out, "name: "+orNone(o.Name)+" → "+orNone(n.Name))
	}
	if o.State != n.State {
		out = append(out, "state: "+orNone(o.State)+" → "+orNone(n.State))
	}
	tagKeys := map[string]bool{}
	for k := range o.Tags {
		tagKeys[k] = true
	}
	for k := range n.Tags {
		tagKeys[k] = true
	}
	sorted := make([]string, 0, len(tagKeys))
	for k := range tagKeys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	for _, k := range sorted {
		ov, inOld := o.Tags[k]
		nv, inNew := n.Tags[k]
		if inOld && inNew && ov == nv {
			continue
		}
		out = append(out, "tag "+k+": "+orNone(ov)+" → "+orNone(nv))
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// Counts summarizes a change list.
func Counts(changes []Change) (added, removed, modified int) {
	for _, c := range changes {
		switch c.Kind {
		case KindAdded:
			added++
		case KindRemoved:
			removed++
		default:
			modified++
		}
	}
	return added, removed, modified
}

// ScopeKey names a baseline file for a region scope: the sorted regions
// joined with "+", or a short hash when that would make an unwieldy filename
// (e.g. --all-regions).
func ScopeKey(regions []string) string {
	scope := append([]string(nil), regions...)
	sort.Strings(scope)
	key := strings.Join(scope, "+")
	if key == "" {
		key = "default"
	}
	if len(key) > 80 {
		h := fnv.New32a()
		h.Write([]byte(key))
		key = fmt.Sprintf("%d-regions-%08x", len(scope), h.Sum32())
	}
	return key
}
