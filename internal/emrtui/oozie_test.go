package emrtui

import (
	"context"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

func TestParseOozieWorkflows(t *testing.T) {
	body := []byte(`{"total":2,"workflows":[
		{"id":"0000001-x-oozie-oozi-W","appName":"nightly-load","status":"SUCCEEDED","user":"hadoop","startTime":"Mon, 15 Jun 2026 01:00:00 GMT","endTime":"Mon, 15 Jun 2026 01:18:00 GMT"},
		{"id":"0000002-x-oozie-oozi-W","appName":"dedupe","status":"KILLED","user":"analyst","startTime":"Mon, 15 Jun 2026 02:00:00 GMT","endTime":""}
	]}`)
	wf, err := parseOozieWorkflows(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(wf) != 2 {
		t.Fatalf("got %d workflows, want 2", len(wf))
	}
	if wf[0].AppName != "nightly-load" || wf[0].Status != "SUCCEEDED" {
		t.Errorf("unexpected wf[0]: %+v", wf[0])
	}
	if wf[1].Status != "KILLED" {
		t.Errorf("wf[1].Status = %q, want KILLED", wf[1].Status)
	}
}

func TestParseOozieWorkflows_Empty(t *testing.T) {
	wf, err := parseOozieWorkflows([]byte(`{"total":0,"workflows":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(wf) != 0 {
		t.Errorf("expected empty, got %d", len(wf))
	}
}

func TestParseOozieCoordinators(t *testing.T) {
	body := []byte(`{"total":1,"coordinatorjobs":[
		{"coordJobId":"0000001-x-oozie-oozi-C","coordJobName":"orders-hourly","status":"RUNNING","frequency":"60","timeUnit":"MINUTE","nextMaterializedTime":"Mon, 15 Jun 2026 20:00:00 GMT","user":"hadoop"}
	]}`)
	coords, err := parseOozieCoordinators(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(coords) != 1 {
		t.Fatalf("got %d coordinators, want 1", len(coords))
	}
	c := coords[0]
	if c.Name != "orders-hourly" || c.Status != "RUNNING" {
		t.Errorf("unexpected coordinator: %+v", c)
	}
	if got := c.frequency(); got != "60 MINUTE" {
		t.Errorf("frequency = %q, want 60 MINUTE", got)
	}
}

func TestOozieCoordinatorFrequency(t *testing.T) {
	if got := (OozieCoordinator{}).frequency(); got != "—" {
		t.Errorf("empty frequency = %q, want em dash", got)
	}
	if got := (OozieCoordinator{Frequency: "5"}).frequency(); got != "5" {
		t.Errorf("no unit frequency = %q, want 5", got)
	}
}

func TestFetchOozie_NilDialerIsDisabled(t *testing.T) {
	_, _, err := FetchOozie(context.Background(), nil, "host")
	if !emrconn.IsUnreachable(err) {
		t.Errorf("nil dialer should be unreachable, got %v", err)
	}
}
