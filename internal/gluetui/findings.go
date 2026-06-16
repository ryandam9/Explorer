package gluetui

import (
	"time"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
)

// glueFindingsSuppressed lists checks the dashboard can't evaluate from the
// loaded inventory and must not emit (guessing would violate the under-warn
// rule). The inventory carries each job's latest run state/start and each
// crawler's last-crawl status — enough for the stale/never-run, latest-run-
// failed and crawler-failed checks — but not a job's security configuration,
// worker count, per-run DPU-seconds, full run history, the VPC network
// inventory or a crawler's running-elapsed, so the checks that need those are
// suppressed here. They remain available in the full `audit` collection.
var glueFindingsSuppressed = map[string]bool{
	findings.CheckGlueNoSecurityConf:  true, // HasSecurityConfig unknown (needs GetJob)
	findings.CheckGlueOversizedWorker: true, // NumberOfWorkers / successful-run unknown
	findings.CheckGlueFailedRunWaste:  true, // per-run DPUSeconds / full history unknown
	findings.CheckGlueConnMissingNet:  true, // needs the EC2 subnet/SG inventory
	findings.CheckGlueCrawlerStuck:    true, // crawler running-elapsed unknown
}

// computeFindings runs the deterministic Glue posture/cost checks over the
// currently loaded inventory, grouped per region. No AWS calls — pure over the
// data already on screen, so the panel is instant and matches what is shown.
func (mm *m) computeFindings() []findings.Finding {
	now := time.Now()
	type bucket struct {
		jobs     []findings.GlueJob
		crawlers []findings.GlueCrawler
	}
	byRegion := map[string]*bucket{}
	get := func(r string) *bucket {
		if b := byRegion[r]; b != nil {
			return b
		}
		b := &bucket{}
		byRegion[r] = b
		return b
	}

	for _, j := range mm.inv.Jobs {
		gj := findings.GlueJob{Name: j.Name, ARN: j.ARN, RunsKnown: true}
		if j.LastRunState != "" {
			gj.Runs = []findings.GlueRun{{
				State: j.LastRunState, Started: j.LastRunStarted, ExecSecs: j.LastRunSeconds,
			}}
		}
		b := get(j.Region)
		b.jobs = append(b.jobs, gj)
	}
	for _, c := range mm.inv.Crawlers {
		b := get(c.Region)
		b.crawlers = append(b.crawlers, findings.GlueCrawler{
			Name: c.Name, ARN: c.ARN, State: c.State, LastCrawlStatus: c.LastCrawlStatus,
		})
	}

	var out []findings.Finding
	for region, b := range byRegion {
		out = append(out, findings.AnalyzeGlue(findings.GlueSnapshot{
			Region: region, Now: now, Jobs: b.jobs, Crawlers: b.crawlers,
		})...)
	}
	out = findings.Drop(out, glueFindingsSuppressed)
	findings.Sort(out)
	return out
}

// findingsColumns is the shared-table column set for the findings panel. REGION
// is appended only in multi-region scope, mirroring the resource tabs.
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

func findingRow(f findings.Finding, multi bool) table.Row {
	r := table.Row{sevLabel(f.Severity), truncate(f.Resource, 22), f.ID, f.Title}
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
