package emrtui

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// oozieJobWindow caps how many jobs each Oozie listing fetches.
const oozieJobWindow = 100

// OozieWorkflow is one workflow job, flattened for the Oozie browser (AXE-042),
// read from the Oozie REST API (default port 11000).
type OozieWorkflow struct {
	ID        string
	AppName   string
	Status    string // SUCCEEDED / RUNNING / KILLED / SUSPENDED / FAILED / PREP…
	User      string
	StartTime string
	EndTime   string
}

// OozieCoordinator is one coordinator job (a schedule that materializes
// workflow runs).
type OozieCoordinator struct {
	ID               string
	Name             string
	Status           string // RUNNING / SUSPENDED / KILLED / PREP / DONEWITHERROR…
	Frequency        string
	TimeUnit         string
	NextMaterialized string
	User             string
}

// --- Oozie REST wire types (only the fields we use) ------------------------

type oozieWorkflowsEnvelope struct {
	Workflows []struct {
		ID        string `json:"id"`
		AppName   string `json:"appName"`
		Status    string `json:"status"`
		User      string `json:"user"`
		StartTime string `json:"startTime"`
		EndTime   string `json:"endTime"`
	} `json:"workflows"`
}

type oozieCoordinatorsEnvelope struct {
	CoordinatorJobs []struct {
		CoordJobID           string `json:"coordJobId"`
		CoordJobName         string `json:"coordJobName"`
		Status               string `json:"status"`
		Frequency            string `json:"frequency"`
		TimeUnit             string `json:"timeUnit"`
		NextMaterializedTime string `json:"nextMaterializedTime"`
		User                 string `json:"user"`
	} `json:"coordinatorjobs"`
}

// parseOozieWorkflows maps an Oozie /jobs?jobtype=wf payload. Pure. A missing
// "workflows" array yields an empty slice, not an error.
func parseOozieWorkflows(body []byte) ([]OozieWorkflow, error) {
	var env oozieWorkflowsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	out := make([]OozieWorkflow, 0, len(env.Workflows))
	for _, w := range env.Workflows {
		out = append(out, OozieWorkflow{
			ID: w.ID, AppName: w.AppName, Status: w.Status, User: w.User,
			StartTime: w.StartTime, EndTime: w.EndTime,
		})
	}
	return out, nil
}

// parseOozieCoordinators maps an Oozie /jobs?jobtype=coordinator payload. Pure.
func parseOozieCoordinators(body []byte) ([]OozieCoordinator, error) {
	var env oozieCoordinatorsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	out := make([]OozieCoordinator, 0, len(env.CoordinatorJobs))
	for _, c := range env.CoordinatorJobs {
		out = append(out, OozieCoordinator{
			ID: c.CoordJobID, Name: c.CoordJobName, Status: c.Status,
			Frequency: c.Frequency, TimeUnit: c.TimeUnit,
			NextMaterialized: c.NextMaterializedTime, User: c.User,
		})
	}
	return out, nil
}

// FetchOozie fetches a cluster's Oozie workflows and coordinators via the
// on-cluster connection layer. Returns an ErrUnreachable-wrapped error when the
// Oozie server can't be reached, so callers render the connect helper.
func FetchOozie(ctx context.Context, d *emrconn.Dialer, masterDNS string) ([]OozieWorkflow, []OozieCoordinator, error) {
	if d == nil {
		return nil, nil, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return nil, nil, fmt.Errorf("%w: cluster has no primary-node DNS (not running?)", emrconn.ErrUnreachable)
	}

	wfPath := fmt.Sprintf("/oozie/v2/jobs?jobtype=wf&len=%d", oozieJobWindow)
	wfBody, err := d.GetRaw(ctx, emrconn.ServiceOozie, masterDNS, wfPath)
	if err != nil {
		return nil, nil, err
	}
	workflows, err := parseOozieWorkflows(wfBody)
	if err != nil {
		return nil, nil, err
	}

	// Coordinators are best-effort: a failure here still returns the workflows.
	var coords []OozieCoordinator
	coordPath := fmt.Sprintf("/oozie/v2/jobs?jobtype=coordinator&len=%d", oozieJobWindow)
	if cBody, cErr := d.GetRaw(ctx, emrconn.ServiceOozie, masterDNS, coordPath); cErr == nil {
		coords, _ = parseOozieCoordinators(cBody)
	}
	return workflows, coords, nil
}

// oozieFrequency renders a coordinator's frequency with its unit, e.g. "60 MINUTE".
func (c OozieCoordinator) frequency() string {
	if c.Frequency == "" {
		return "—"
	}
	if c.TimeUnit == "" {
		return c.Frequency
	}
	return c.Frequency + " " + c.TimeUnit
}
