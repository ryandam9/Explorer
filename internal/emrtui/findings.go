package emrtui

import (
	"time"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// emrFindingsSuppressed lists check IDs the dashboard must not emit because it
// can't evaluate them from the loaded inventory (it would otherwise have to
// guess, violating the under-warn rule). The cluster inventory carries every
// field the live-cluster checks need; only the latest-step check depends on
// per-cluster step history, which is loaded lazily — AnalyzeEMR already
// self-silences that via StepsKnown=false, so nothing needs suppressing today.
// Kept as the single, documented seam for when that changes.
var emrFindingsSuppressed = map[string]bool{}

// computeFindings runs the deterministic EMR posture/cost checks over the
// currently loaded inventory, grouped per region. It makes no AWS calls — it is
// pure over the data already on screen, so the panel is instant and matches
// exactly what the user is looking at (active clusters, or all states when the
// terminated tail is toggled on).
func (mm *m) computeFindings() []findings.Finding {
	now := time.Now()
	byRegion := map[string][]findings.EMRCluster{}
	for _, c := range mm.inv.Clusters {
		byRegion[c.Region] = append(byRegion[c.Region], findings.EMRCluster{
			ID:                c.ID,
			Name:              c.Name,
			ARN:               c.ARN,
			State:             c.State,
			Created:           c.Created,
			AutoTerminate:     c.AutoTerminate,
			HasLogURI:         c.LogURI != "",
			HasSecurityConfig: c.SecurityConfig != "",
			StepsKnown:        false, // step history is loaded lazily, per cluster
		})
	}
	var out []findings.Finding
	for region, clusters := range byRegion {
		out = append(out, findings.AnalyzeEMR(findings.EMRSnapshot{
			Region: region, Now: now, Clusters: clusters,
		})...)
	}
	out = findings.Drop(out, emrFindingsSuppressed)
	findings.Sort(out)
	return out
}

// findingsColumns is the shared-table column set for the findings panel. REGION
// is appended only in multi-region scope, mirroring the cluster list.
func findingsColumns(multi bool) []table.Column {
	cols := []table.Column{
		{Title: "SEVERITY", Width: 10},
		{Title: "RESOURCE", Width: 14},
		{Title: "CHECK", Width: 13},
		{Title: "TITLE", Width: 24},
	}
	if multi {
		cols = append(cols, table.Column{Title: "REGION", Width: 9})
	}
	return cols
}

// findingRow renders one finding as a shared-table row.
func findingRow(f findings.Finding, multi bool) table.Row {
	r := table.Row{sevLabel(f.Severity), truncate(f.Resource, 22), f.ID, f.Title}
	if multi {
		r = append(r, f.Region)
	}
	return r
}

// sevLabel renders a severity with a leading glyph (the shared table styles
// cells uniformly, so the glyph carries the at-a-glance signal). ASCII-friendly
// text per the fixed-width-header convention.
func sevLabel(s findings.Severity) string {
	switch s {
	case findings.SevCritical:
		return "✗ CRITICAL"
	case findings.SevWarning:
		return "● WARNING"
	default:
		return "• INFO"
	}
}

// selectedFinding returns the highlighted finding in the panel.
func (mm *m) selectedFinding() (findings.Finding, bool) {
	i := mm.findingsTbl.Cursor()
	if i < 0 || i >= len(mm.findingList) {
		return findings.Finding{}, false
	}
	return mm.findingList[i], true
}
