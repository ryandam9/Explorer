package emrtui

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// YarnApp is one YARN application from the ResourceManager REST API, flattened
// for the YARN browser (AXE-040).
type YarnApp struct {
	ID          string
	Name        string
	State       string // RUNNING / FINISHED / FAILED / KILLED / ACCEPTED…
	FinalStatus string // SUCCEEDED / FAILED / UNDEFINED…
	Progress    float64
	Queue       string
	User        string
	Type        string
	ElapsedMS   int64
}

// ClusterMetrics is the YARN cluster's resource picture (the browser footer).
type ClusterMetrics struct {
	AppsRunning   int
	AllocatedMB   int64
	TotalMB       int64
	AllocatedVCfg int64
	TotalVC       int64
}

// --- RM REST wire types (only the fields we use) ---------------------------

type rmAppsEnvelope struct {
	Apps *struct {
		App []rmApp `json:"app"`
	} `json:"apps"`
}

type rmApp struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	User            string  `json:"user"`
	Queue           string  `json:"queue"`
	State           string  `json:"state"`
	FinalStatus     string  `json:"finalStatus"`
	Progress        float64 `json:"progress"`
	ApplicationType string  `json:"applicationType"`
	ElapsedTime     int64   `json:"elapsedTime"`
	StartedTime     int64   `json:"startedTime"`
}

type rmMetricsEnvelope struct {
	ClusterMetrics *struct {
		AppsRunning           int   `json:"appsRunning"`
		AllocatedMB           int64 `json:"allocatedMB"`
		TotalMB               int64 `json:"totalMB"`
		AllocatedVirtualCores int64 `json:"allocatedVirtualCores"`
		TotalVirtualCores     int64 `json:"totalVirtualCores"`
	} `json:"clusterMetrics"`
}

// parseYarnApps maps an RM /ws/v1/cluster/apps payload to YarnApps, newest
// first. Pure, so it is fixture-tested. A null "apps" (no applications) yields
// an empty slice, not an error.
func parseYarnApps(body []byte) ([]YarnApp, error) {
	var env rmAppsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	if env.Apps == nil {
		return nil, nil
	}
	out := make([]YarnApp, 0, len(env.Apps.App))
	for _, a := range env.Apps.App {
		out = append(out, YarnApp{
			ID: a.ID, Name: a.Name, State: a.State, FinalStatus: a.FinalStatus,
			Progress: a.Progress, Queue: a.Queue, User: a.User,
			Type: a.ApplicationType, ElapsedMS: a.ElapsedTime,
		})
	}
	// Newest first by start time (RM returns them unordered).
	rawStart := make(map[string]int64, len(env.Apps.App))
	for _, a := range env.Apps.App {
		rawStart[a.ID] = a.StartedTime
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rawStart[out[i].ID] > rawStart[out[j].ID]
	})
	return out, nil
}

// parseClusterMetrics maps an RM /ws/v1/cluster/metrics payload. Pure.
func parseClusterMetrics(body []byte) (ClusterMetrics, error) {
	var env rmMetricsEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return ClusterMetrics{}, err
	}
	if env.ClusterMetrics == nil {
		return ClusterMetrics{}, nil
	}
	m := env.ClusterMetrics
	return ClusterMetrics{
		AppsRunning: m.AppsRunning, AllocatedMB: m.AllocatedMB, TotalMB: m.TotalMB,
		AllocatedVCfg: m.AllocatedVirtualCores, TotalVC: m.TotalVirtualCores,
	}, nil
}

// FetchYARN fetches a cluster's live YARN applications and cluster metrics via
// the on-cluster connection layer. Returns an ErrUnreachable-wrapped error when
// the RM can't be reached, so callers render the connect helper.
func FetchYARN(ctx context.Context, d *emrconn.Dialer, masterDNS string) ([]YarnApp, ClusterMetrics, error) {
	if d == nil {
		return nil, ClusterMetrics{}, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return nil, ClusterMetrics{}, fmt.Errorf("%w: cluster has no primary-node DNS (not running?)", emrconn.ErrUnreachable)
	}
	appsBody, err := d.GetRaw(ctx, emrconn.ServiceYARN, masterDNS, "/ws/v1/cluster/apps")
	if err != nil {
		return nil, ClusterMetrics{}, err
	}
	apps, err := parseYarnApps(appsBody)
	if err != nil {
		return nil, ClusterMetrics{}, err
	}
	var metrics ClusterMetrics
	if mBody, mErr := d.GetRaw(ctx, emrconn.ServiceYARN, masterDNS, "/ws/v1/cluster/metrics"); mErr == nil {
		metrics, _ = parseClusterMetrics(mBody)
	}
	return apps, metrics, nil
}

// elapsed renders a YARN app's elapsed time (ms) as a duration.
func (a YarnApp) elapsed() string {
	if a.ElapsedMS <= 0 {
		return "—"
	}
	return formatSeconds(int32((time.Duration(a.ElapsedMS) * time.Millisecond).Seconds()))
}
