package lambdatui

import (
	"time"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// computeFindings runs the deterministic Lambda checks over the currently
// loaded functions, grouped per region. No AWS calls — pure over the data
// already on screen (the list, plus the background usage/posture load), so the
// panel is instant and matches what is shown. A fact the usage load couldn't
// read stays "unknown" and silences the checks that need it.
func (mm *m) computeFindings() []findings.Finding {
	now := time.Now()
	byRegion := map[string][]findings.LambdaFunction{}
	for _, f := range mm.inv.Functions {
		lf := findings.LambdaFunction{
			Name:             f.Name,
			ARN:              f.ARN,
			Runtime:          f.Runtime,
			PackageType:      f.PackageType,
			HasDLQ:           f.DLQTargetArn != "",
			StateKnown:       f.State != "",
			State:            f.State,
			LastUpdateStatus: f.LastUpdateStatus,
			Architectures:    f.Architectures,
			HasLayers:        len(f.Layers) > 0,
			LastModified:     f.LastModified,
			LogGroup:         f.LogGroup,
		}
		if u, ok := mm.usage[usageKey(f.Region, f.Name)]; ok {
			lf.UsageKnown, lf.Invocations30d = u.UsageKnown, u.Invocations30d
			lf.LogGroupKnown, lf.LogGroupExists, lf.RetentionDays, lf.StoredBytes = u.LogKnown, u.LogExists, u.RetentionDays, u.StoredBytes
			lf.PolicyKnown, lf.Policy = u.PolicyKnown, u.Policy
			lf.URLKnown, lf.URLAuthTypes = u.URLKnown, u.URLAuthTypes
		}
		byRegion[f.Region] = append(byRegion[f.Region], lf)
	}

	var out []findings.Finding
	for region, fns := range byRegion {
		out = append(out, findings.AnalyzeLambda(findings.LambdaSnapshot{
			Region: region, Now: now, Functions: fns,
		})...)
	}
	findings.Sort(out)
	return out
}

// findingsColumns is the shared-table column set for the findings panel. REGION
// is appended only in multi-region scope, mirroring the resource tabs.
func findingsColumns(multi bool) []table.Column {
	cols := []table.Column{
		{Title: "SEVERITY", Width: 10},
		{Title: "RESOURCE", Width: 16},
		{Title: "CHECK", Width: 12},
		{Title: "TITLE", Width: 28},
	}
	if multi {
		cols = append(cols, table.Column{Title: "REGION", Width: 9})
	}
	return cols
}

func findingRow(f findings.Finding, multi bool) table.Row {
	r := table.Row{sevLabel(f.Severity), truncate(f.Resource, 24), f.ID, f.Title}
	if multi {
		r = append(r, f.Region)
	}
	return r
}

// sevLabel renders a severity with a leading glyph (the shared table styles
// cells uniformly, so the glyph carries the at-a-glance signal).
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
