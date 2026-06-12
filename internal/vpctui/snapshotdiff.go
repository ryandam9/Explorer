package vpctui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Snapshot diff ("it worked yesterday")
//
// A VPC snapshot can be saved to disk as a baseline and later diffed against the
// live VPC to see exactly what changed — added/removed resources and, for
// resources that still exist, the specific facts (rules, routes, attributes)
// that were added or removed. The diff is a pure function over two snapshots.
// ---------------------------------------------------------------------------

type changeKind int

const (
	changeAdded changeKind = iota
	changeRemoved
	changeModified
)

func (k changeKind) glyph() string {
	switch k {
	case changeAdded:
		return "+"
	case changeRemoved:
		return "-"
	default:
		return "~"
	}
}

// snapshotChange is one difference between two snapshots.
type snapshotChange struct {
	Kind    changeKind
	Type    string   // resource type label, e.g. "Security group"
	ID      string   // resource ID
	Added   []string // facts present only in the new snapshot (modified only)
	Removed []string // facts present only in the old snapshot (modified only)
}

// diffSection pairs a resource-type label with a function that fingerprints each
// resource of that type into a set of comparable facts.
type diffSection struct {
	name  string
	facts func(vpcSnapshot) map[string][]string
}

// diffSnapshots returns the ordered list of changes turning old into new.
func diffSnapshots(old, neu vpcSnapshot) []snapshotChange {
	var out []snapshotChange
	for _, sec := range diffSections() {
		diffOneSection(sec.name, sec.facts(old), sec.facts(neu), &out)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func diffOneSection(typeName string, oldM, newM map[string][]string, out *[]snapshotChange) {
	ids := map[string]bool{}
	for id := range oldM {
		ids[id] = true
	}
	for id := range newM {
		ids[id] = true
	}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)

	for _, id := range sorted {
		o, inOld := oldM[id]
		n, inNew := newM[id]
		switch {
		case inNew && !inOld:
			*out = append(*out, snapshotChange{Kind: changeAdded, Type: typeName, ID: id})
		case inOld && !inNew:
			*out = append(*out, snapshotChange{Kind: changeRemoved, Type: typeName, ID: id})
		default:
			added, removed := factDiff(o, n)
			if len(added) > 0 || len(removed) > 0 {
				*out = append(*out, snapshotChange{
					Kind: changeModified, Type: typeName, ID: id, Added: added, Removed: removed,
				})
			}
		}
	}
}

// factDiff returns the facts added in neu and removed from old.
func factDiff(old, neu []string) (added, removed []string) {
	om := map[string]bool{}
	for _, f := range old {
		om[f] = true
	}
	nm := map[string]bool{}
	for _, f := range neu {
		nm[f] = true
	}
	for _, f := range neu {
		if !om[f] {
			added = append(added, f)
		}
	}
	for _, f := range old {
		if !nm[f] {
			removed = append(removed, f)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// diffSections defines how each resource type is fingerprinted. Volatile fields
// like available-IP counts are deliberately excluded to avoid noisy diffs.
func diffSections() []diffSection {
	return []diffSection{
		{"Security group", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, sg := range s.SecurityGroups {
				var f []string
				for _, r := range sg.Rules {
					f = append(f, ruleKey(r))
				}
				sort.Strings(f)
				m[sg.ID] = f
			}
			return m
		}},
		{"Route table", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, rt := range s.RouteTables {
				var f []string
				for _, r := range rt.Routes {
					f = append(f, fmt.Sprintf("%s -> %s (%s)", r.Destination, r.Target, r.State))
				}
				for _, a := range rt.Associations {
					f = append(f, "assoc "+a)
				}
				sort.Strings(f)
				m[rt.ID] = f
			}
			return m
		}},
		{"Subnet", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, sn := range s.Subnets {
				m[sn.ID] = []string{
					"cidr=" + sn.CIDR,
					"az=" + sn.AZ,
					fmt.Sprintf("mapPublicIp=%t", sn.MapPublicIPOnLaunch),
				}
			}
			return m
		}},
		{"Network ACL", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, n := range s.NetworkACLs {
				var f []string
				for _, r := range n.Rules {
					f = append(f, fmt.Sprintf("%s %d %s %s %s %s",
						strings.ToLower(r.Direction), r.RuleNumber, r.Action, r.Protocol, r.PortRange, r.CIDR))
				}
				// A NACL re-association is a classic silent breaker.
				for _, a := range n.Associations {
					f = append(f, "assoc "+a)
				}
				sort.Strings(f)
				for i := range f {
					m[n.ID] = append(m[n.ID], f[i])
				}
				if m[n.ID] == nil {
					m[n.ID] = []string{}
				}
			}
			return m
		}},
		{"NAT gateway", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, n := range s.NatGateways {
				m[n.ID] = []string{"state=" + n.State, "subnet=" + n.SubnetID, "publicIp=" + n.PublicIP}
			}
			return m
		}},
		{"Internet gateway", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, g := range s.InternetGateways {
				m[g.ID] = []string{"state=" + g.State, "vpc=" + g.VPCID}
			}
			return m
		}},
		{"Peering", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, p := range s.Peerings {
				m[p.ID] = []string{"status=" + p.Status}
			}
			return m
		}},
		{"VPC endpoint", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, e := range s.Endpoints {
				f := []string{"state=" + e.State, fmt.Sprintf("privateDns=%t", e.PrivateDNSEnabled)}
				for _, rt := range e.RouteTableIDs {
					f = append(f, "rt "+rt)
				}
				for _, sg := range e.SecurityGroups {
					f = append(f, "sg "+sg)
				}
				for _, sn := range e.SubnetIDs {
					f = append(f, "subnet "+sn)
				}
				sort.Strings(f)
				m[e.ID] = f
			}
			return m
		}},
		{"Network interface", func(s vpcSnapshot) map[string][]string {
			m := map[string][]string{}
			for _, e := range s.NetworkInterfaces {
				sgs := append([]string(nil), e.SecurityGroups...)
				sort.Strings(sgs)
				m[e.ID] = []string{
					"subnet=" + e.SubnetID,
					"privateIp=" + e.PrivateIP,
					"publicIp=" + e.PublicIP,
					"sgs=" + strings.Join(sgs, ","),
				}
			}
			return m
		}},
	}
}

// diffCounts summarizes a change list.
func diffCounts(changes []snapshotChange) (added, removed, modified int) {
	for _, c := range changes {
		switch c.Kind {
		case changeAdded:
			added++
		case changeRemoved:
			removed++
		default:
			modified++
		}
	}
	return added, removed, modified
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// snapshotDir returns the directory where baseline snapshots are stored,
// creating it if necessary.
func snapshotDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".aws_explorer", "vpc-snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// snapshotPath returns the baseline file for a VPC. The owning account is part
// of the name so that VPCs from different accounts/profiles cannot collide.
func snapshotPath(dir, vpcID, ownerID string) string {
	name := vpcID + ".json"
	if ownerID != "" {
		name = ownerID + "-" + name
	}
	return filepath.Join(dir, name)
}

// saveSnapshot writes a baseline snapshot for a VPC to disk.
func saveSnapshot(snap vpcSnapshot) error {
	dir, err := snapshotDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(snapshotPath(dir, snap.VPCID, snap.OwnerID), data, 0o644)
}

// loadSnapshot reads a baseline snapshot for a VPC. The bool is false when no
// baseline has been saved yet. Baselines written before account scoping
// (vpcID.json) are still found as a fallback.
func loadSnapshot(vpcID, ownerID string) (vpcSnapshot, bool, error) {
	dir, err := snapshotDir()
	if err != nil {
		return vpcSnapshot{}, false, err
	}
	data, err := os.ReadFile(snapshotPath(dir, vpcID, ownerID))
	if os.IsNotExist(err) && ownerID != "" {
		data, err = os.ReadFile(snapshotPath(dir, vpcID, ""))
	}
	if os.IsNotExist(err) {
		return vpcSnapshot{}, false, nil
	}
	if err != nil {
		return vpcSnapshot{}, false, err
	}
	var snap vpcSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return vpcSnapshot{}, false, err
	}
	return snap, true, nil
}
