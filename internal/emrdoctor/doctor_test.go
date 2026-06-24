package emrdoctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

func TestParseServices(t *testing.T) {
	all := AllServices()
	for _, in := range []string{"", "all", "ALL", "  "} {
		got, err := ParseServices(in)
		if err != nil || len(got) != len(all) {
			t.Errorf("ParseServices(%q) = %v, %v; want all", in, got, err)
		}
	}
	// Explicit list is returned in canonical order regardless of input order.
	got, err := ParseServices("oozie,hbase")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "hbase,oozie" {
		t.Errorf("ParseServices(oozie,hbase) = %v; want [hbase oozie] in canonical order", got)
	}
	if _, err := ParseServices("hbase,bogus"); err == nil {
		t.Error("unknown service should error")
	}
}

func TestClusterUsable(t *testing.T) {
	for _, s := range []string{"RUNNING", "WAITING", "running", " waiting "} {
		if !clusterUsable(s) {
			t.Errorf("clusterUsable(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "TERMINATED", "TERMINATING", "STARTING", "BOOTSTRAPPING"} {
		if clusterUsable(s) {
			t.Errorf("clusterUsable(%q) = true, want false", s)
		}
	}
}

func TestTunnelFailureClassification(t *testing.T) {
	cases := []struct {
		name     string
		err      string
		wantHint string // a fragment expected in the hint
	}{
		{"auth", "ssh: handshake failed: ssh: unable to authenticate", "key or user is wrong"},
		{"badkey", "parse ssh key \"x\": ...", "key or user is wrong"},
		{"timeout", "dial tcp 1.2.3.4:22: i/o timeout", "Port 22 is not reachable"},
		{"refused", "dial tcp 1.2.3.4:22: connect: connection refused", "Port 22 is closed"},
		{"other", "some unexpected failure", "Verify network reachability"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, hint := tunnelFailure(errors.New(tc.err), "host")
			if !strings.Contains(hint, tc.wantHint) {
				t.Errorf("tunnelFailure(%q) hint = %q, missing %q", tc.err, hint, tc.wantHint)
			}
		})
	}
}

func TestReportCountsAndFailed(t *testing.T) {
	r := &Report{}
	r.ok("a", "")
	r.warn("b", "", "h")
	r.fail("c", "", "h")
	r.skip("d", "")
	ok, fail, warn, skip := r.Counts()
	if ok != 1 || fail != 1 || warn != 1 || skip != 1 {
		t.Errorf("Counts = %d/%d/%d/%d, want 1/1/1/1", ok, fail, warn, skip)
	}
	if !r.Failed() {
		t.Error("Failed() should be true when a check failed")
	}
	r2 := &Report{}
	r2.ok("a", "")
	if r2.Failed() {
		t.Error("Failed() should be false with no failures")
	}
}

// When on-cluster access is off, Run fails the config check and skips every
// downstream layer — it must never claim to have verified a bridge or daemon it
// could not reach.
func TestRun_DisabledShortCircuits(t *testing.T) {
	r := Run(context.Background(), config.OnClusterConfig{Mode: "off"},
		ClusterInfo{State: "RUNNING", PrimaryDNS: "primary.example"}, AllServices())

	if !r.Failed() {
		t.Fatal("disabled config should fail")
	}
	if r.Checks[0].Name != "on-cluster config" || r.Checks[0].Status != StatusFail {
		t.Errorf("first check = %+v, want failed on-cluster config", r.Checks[0])
	}
	// The cluster check is pure AWS and may pass, but no bridge or daemon check
	// may report OK — those depend on a dialer that couldn't be built, so they
	// must all be skipped (never claim to have verified an unreachable layer).
	for _, c := range r.Checks {
		if c.Name == "cluster" {
			continue
		}
		if c.Status == StatusOK {
			t.Errorf("no bridge/daemon check should pass when access is off, but %q is OK", c.Name)
		}
	}
	// Every requested service must appear as a skip line.
	for _, svc := range []string{"HBase", "YARN", "Oozie", "Hive"} {
		var found bool
		for _, c := range r.Checks {
			if c.Name == svc && c.Status == StatusSkip {
				found = true
			}
		}
		if !found {
			t.Errorf("service %q should be skipped when access is off", svc)
		}
	}
}

// A DescribeCluster failure is surfaced as a failed cluster check, not a panic
// or an abort.
func TestRun_DescribeErrorReported(t *testing.T) {
	r := Run(context.Background(), config.OnClusterConfig{Mode: "off"},
		ClusterInfo{DescribeErr: errors.New("AccessDenied")}, []string{"hbase"})
	var sawCluster bool
	for _, c := range r.Checks {
		if c.Name == "cluster" {
			sawCluster = true
			if c.Status != StatusFail || !strings.Contains(c.Detail, "DescribeCluster failed") {
				t.Errorf("cluster check = %+v, want failed with DescribeCluster message", c)
			}
		}
	}
	if !sawCluster {
		t.Error("expected a cluster check in the report")
	}
}
