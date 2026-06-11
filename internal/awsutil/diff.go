package awsutil

import (
	"fmt"

	"github.com/user/aws_explorer/internal/model"
)

// ModifiedResourceDiff holds facts of a changed resource.
type ModifiedResourceDiff struct {
	ResourceOld model.Resource
	ResourceNew model.Resource
	Changes     []string
}

// ResourceDiff represents the complete inventory difference between two scans.
type ResourceDiff struct {
	Added    []model.Resource
	Removed  []model.Resource
	Modified []ModifiedResourceDiff
}

// DiffScans compares two slices of resources to produce an inventory diff.
func DiffScans(old, neu []model.Resource) ResourceDiff {
	oldMap := make(map[string]model.Resource)
	for _, r := range old {
		key := r.ARN
		if key == "" {
			key = r.Service + "/" + r.Type + "/" + r.Region + "/" + r.ID
		}
		oldMap[key] = r
	}

	newMap := make(map[string]model.Resource)
	for _, r := range neu {
		key := r.ARN
		if key == "" {
			key = r.Service + "/" + r.Type + "/" + r.Region + "/" + r.ID
		}
		newMap[key] = r
	}

	var diff ResourceDiff

	for key, n := range newMap {
		o, exists := oldMap[key]
		if !exists {
			diff.Added = append(diff.Added, n)
		} else {
			var changes []string
			if o.State != n.State {
				changes = append(changes, fmt.Sprintf("State: %q -> %q", o.State, n.State))
			}
			// Compare summaries
			for sk, sv := range n.Summary {
				oldSv, ok := o.Summary[sk]
				if !ok {
					changes = append(changes, fmt.Sprintf("Summary added [%s]: %q", sk, sv))
				} else if oldSv != sv {
					changes = append(changes, fmt.Sprintf("Summary changed [%s]: %q -> %q", sk, oldSv, sv))
				}
			}
			for sk := range o.Summary {
				if _, ok := n.Summary[sk]; !ok {
					changes = append(changes, fmt.Sprintf("Summary removed [%s]", sk))
				}
			}
			if len(changes) > 0 {
				diff.Modified = append(diff.Modified, ModifiedResourceDiff{
					ResourceOld: o,
					ResourceNew: n,
					Changes:     changes,
				})
			}
		}
	}

	for key, o := range oldMap {
		if _, exists := newMap[key]; !exists {
			diff.Removed = append(diff.Removed, o)
		}
	}

	return diff
}

// BuildDiffResources translates a ResourceDiff into visual virtual resources for display.
func BuildDiffResources(diff ResourceDiff) []model.Resource {
	var resources []model.Resource
	for _, r := range diff.Added {
		resources = append(resources, model.Resource{
			Service: "Diff",
			Type:    r.Service + "/" + r.Type,
			Region:  r.Region,
			ID:      r.ID,
			Name:    r.Name,
			State:   "ADDED",
			Details: map[string]any{
				"diffKind": "ADDED",
				"arn":      r.ARN,
				"raw":      r,
			},
		})
	}
	for _, r := range diff.Removed {
		resources = append(resources, model.Resource{
			Service: "Diff",
			Type:    r.Service + "/" + r.Type,
			Region:  r.Region,
			ID:      r.ID,
			Name:    r.Name,
			State:   "REMOVED",
			Details: map[string]any{
				"diffKind": "REMOVED",
				"arn":      r.ARN,
				"raw":      r,
			},
		})
	}
	for _, m := range diff.Modified {
		r := m.ResourceNew
		resources = append(resources, model.Resource{
			Service: "Diff",
			Type:    r.Service + "/" + r.Type,
			Region:  r.Region,
			ID:      r.ID,
			Name:    r.Name,
			State:   "MODIFIED",
			Details: map[string]any{
				"diffKind": "MODIFIED",
				"changes":  m.Changes,
				"arn":      r.ARN,
				"rawOld":   m.ResourceOld,
				"rawNew":   m.ResourceNew,
			},
		})
	}
	return resources
}
