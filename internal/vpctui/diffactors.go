package vpctui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/trail"
)

// ---------------------------------------------------------------------------
// Snapshot-diff actor attribution ("who changed this?")
//
// Inside the w "what changed" overlay, t fetches the most recent CloudTrail
// mutation event for each changed resource and annotates the diff lines with
// the likely actor. Attribution is best-effort: CloudTrail's LookupEvents
// covers 90 days of management events, is rate-limited (2 TPS, hence the
// serial lookups), and matches by resource name — so the annotation names the
// *latest* actor, who is the likely but not guaranteed author of the diff.
// ---------------------------------------------------------------------------

// maxActorLookups caps how many changed resources are attributed in one go;
// at 2 TPS a bigger diff would keep the overlay spinning for too long.
const maxActorLookups = 15

// actorLookupInterval keeps the serial LookupEvents calls under the service's
// 2 TPS limit.
const actorLookupInterval = 600 * time.Millisecond

type diffActorsDoneMsg struct {
	vpcID   string
	actors  map[string]trail.Event // resource ID → latest mutation event
	note    string                 // degradation note (denied, partial, …)
	skipped int                    // changes beyond the lookup cap
}

// diffActorTargets returns the unique resource IDs from the change list, in
// order, capped at max. The second result is how many were left out.
func diffActorTargets(changes []snapshotChange, max int) (targets []string, skipped int) {
	seen := map[string]bool{}
	for _, c := range changes {
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if len(targets) < max {
			targets = append(targets, c.ID)
		} else {
			skipped++
		}
	}
	return targets, skipped
}

// formatActor renders one attribution line for the diff overlay.
func formatActor(ev trail.Event) string {
	return "by " + ev.Principal + " — " + ev.EventName + ", " +
		ev.Time.UTC().Format("2006-01-02 15:04 MST")
}

// loadDiffActors fetches the latest CloudTrail mutation for each changed
// resource, serially to respect the LookupEvents rate limit.
func (m *Model) loadDiffActors() tea.Cmd {
	if m.selectedVPC == nil || len(m.snapDiff) == 0 {
		return nil
	}
	m.diffActorsLoading = true
	m.diffVP.SetContent(m.renderDiff())

	client := m.client
	region := m.selectedVPC.Region
	vpcID := m.selectedVPC.ID
	targets, skipped := diffActorTargets(m.snapDiff, maxActorLookups)

	return func() tea.Msg {
		actors := map[string]trail.Event{}
		for i, id := range targets {
			if i > 0 {
				select {
				case <-client.ctx.Done():
					return diffActorsDoneMsg{vpcID: vpcID, actors: actors, note: "lookup cancelled", skipped: skipped}
				case <-time.After(actorLookupInterval):
				}
			}
			ctx, cancel := context.WithTimeout(client.ctx, awsRequestTimeout)
			events, err := trail.Lookup(ctx, client.cfg, region, id, trail.Options{Limit: 1})
			cancel()
			if err != nil {
				note := "CloudTrail lookup failed: " + err.Error()
				if awserr.IsAuthError(err) {
					note = "CloudTrail lookup denied — grant cloudtrail:LookupEvents to attribute changes"
				}
				return diffActorsDoneMsg{vpcID: vpcID, actors: actors, note: note, skipped: skipped}
			}
			if len(events) > 0 {
				actors[id] = events[0]
			}
		}
		return diffActorsDoneMsg{vpcID: vpcID, actors: actors, skipped: skipped}
	}
}
