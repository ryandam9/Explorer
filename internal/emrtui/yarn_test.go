package emrtui

import (
	"context"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

func TestParseYarnApps(t *testing.T) {
	body := []byte(`{"apps":{"app":[
		{"id":"application_1_0001","user":"hadoop","name":"job-a","queue":"default","state":"FINISHED","finalStatus":"SUCCEEDED","progress":100.0,"applicationType":"SPARK","elapsedTime":120000,"startedTime":1000},
		{"id":"application_1_0002","user":"analyst","name":"job-b","queue":"adhoc","state":"RUNNING","finalStatus":"UNDEFINED","progress":63.0,"applicationType":"SPARK","elapsedTime":30000,"startedTime":3000}
	]}}`)
	apps, err := parseYarnApps(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}
	// Newest first by startedTime → 0002 (started 3000) before 0001 (1000).
	if apps[0].ID != "application_1_0002" {
		t.Errorf("expected newest first, got %s", apps[0].ID)
	}
	if apps[0].State != "RUNNING" || apps[0].Progress != 63.0 || apps[0].Queue != "adhoc" {
		t.Errorf("unexpected app[0]: %+v", apps[0])
	}
}

func TestParseYarnApps_NullAppsIsEmpty(t *testing.T) {
	apps, err := parseYarnApps([]byte(`{"apps":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 0 {
		t.Errorf("null apps should yield empty, got %d", len(apps))
	}
}

func TestParseClusterMetrics(t *testing.T) {
	body := []byte(`{"clusterMetrics":{"appsRunning":2,"allocatedMB":184320,"totalMB":262144,"allocatedVirtualCores":40,"totalVirtualCores":64}}`)
	m, err := parseClusterMetrics(body)
	if err != nil {
		t.Fatal(err)
	}
	if m.AppsRunning != 2 || m.AllocatedMB != 184320 || m.TotalVC != 64 {
		t.Errorf("unexpected metrics: %+v", m)
	}
}

func TestFetchYARN_NilDialerIsDisabled(t *testing.T) {
	_, _, err := FetchYARN(context.Background(), nil, "host")
	if !emrconn.IsUnreachable(err) {
		t.Errorf("nil dialer should be unreachable, got %v", err)
	}
}

func TestYarnAppElapsed(t *testing.T) {
	if got := (YarnApp{ElapsedMS: 82000}).elapsed(); got != "1m 22s" {
		t.Errorf("elapsed = %q, want 1m 22s", got)
	}
	if got := (YarnApp{ElapsedMS: 0}).elapsed(); got != "—" {
		t.Errorf("zero elapsed = %q, want em dash", got)
	}
}
