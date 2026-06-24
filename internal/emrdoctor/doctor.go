// Package emrdoctor implements `emr connect-check`: a step-by-step verifier for
// the opt-in on-cluster connection layer (YARN / HBase / Oozie / Hive). It walks
// the same layers a real connection uses — config, cluster, bridge (SSH tunnel /
// SOCKS / direct), then per-daemon port and protocol health — and reports a
// pass/fail line with a concrete fix at each step. A failure short-circuits the
// layers that depend on it (with skip lines) so the output never implies it
// verified something it couldn't reach. Every probe is read-only and bounded.
package emrdoctor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// DefaultHivePort is the HiveServer2 Thrift port. Hive is checked at the TCP
// level only — HiveServer2 speaks Thrift, not the HTTP the dialer uses — so the
// doctor can confirm the port is reachable but not that the service is healthy.
const DefaultHivePort = 10000

// Status is the outcome of one verification step.
type Status string

const (
	StatusOK   Status = "ok"
	StatusFail Status = "fail"
	StatusWarn Status = "warn"
	StatusSkip Status = "skip"
)

// Check is one verification step's outcome.
type Check struct {
	Name   string
	Status Status
	Detail string
	Hint   string // the concrete next action; shown on fail/warn only
}

// Report is the ordered list of checks the doctor produced.
type Report struct {
	Checks []Check
}

func (r *Report) add(c Check)            { r.Checks = append(r.Checks, c) }
func (r *Report) ok(name, detail string) { r.add(Check{Name: name, Status: StatusOK, Detail: detail}) }
func (r *Report) fail(name, detail, hint string) {
	r.add(Check{Name: name, Status: StatusFail, Detail: detail, Hint: hint})
}
func (r *Report) warn(name, detail, hint string) {
	r.add(Check{Name: name, Status: StatusWarn, Detail: detail, Hint: hint})
}
func (r *Report) skip(name, detail string) {
	r.add(Check{Name: name, Status: StatusSkip, Detail: detail})
}

// Failed reports whether any check failed (drives the command's exit behavior).
func (r *Report) Failed() bool {
	for _, c := range r.Checks {
		if c.Status == StatusFail {
			return true
		}
	}
	return false
}

// Counts tallies how many checks landed in each status.
func (r *Report) Counts() (ok, fail, warn, skip int) {
	for _, c := range r.Checks {
		switch c.Status {
		case StatusOK:
			ok++
		case StatusFail:
			fail++
		case StatusWarn:
			warn++
		case StatusSkip:
			skip++
		}
	}
	return
}

// target describes one on-cluster service the doctor can probe.
type target struct {
	label    string
	svc      emrconn.Service // the dialer service (empty for port-only targets)
	port     int             // default daemon port
	health   string          // HTTP health path (empty for port-only targets)
	portOnly bool            // Hive: TCP reachability only, no protocol check
}

var targets = map[string]target{
	"hbase": {"HBase", emrconn.ServiceHBase, emrconn.DefaultHBasePort, "/version/cluster", false},
	"yarn":  {"YARN", emrconn.ServiceYARN, emrconn.DefaultYARNPort, "/ws/v1/cluster/info", false},
	"oozie": {"Oozie", emrconn.ServiceOozie, emrconn.DefaultOoziePort, "/oozie/v1/admin/status", false},
	"hive":  {"Hive", "", DefaultHivePort, "", true},
}

// serviceOrder is the canonical display order.
var serviceOrder = []string{"hbase", "yarn", "oozie", "hive"}

// AllServices returns the selectable service keys in display order.
func AllServices() []string { return append([]string(nil), serviceOrder...) }

// ParseServices resolves the --service value to known service keys in canonical
// order. "" or "all" selects every service.
func ParseServices(s string) ([]string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "all" {
		return AllServices(), nil
	}
	want := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := targets[part]; !ok {
			return nil, fmt.Errorf("unknown service %q (valid: hbase, yarn, oozie, hive, or all)", part)
		}
		want[part] = true
	}
	if len(want) == 0 {
		return AllServices(), nil
	}
	var out []string
	for _, k := range serviceOrder {
		if want[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// ClusterInfo is the AWS-side cluster facts the doctor needs, resolved by the
// caller with one DescribeCluster. DescribeErr is non-nil when that call failed.
type ClusterInfo struct {
	State       string
	PrimaryDNS  string
	DescribeErr error
}

// Run performs the layered connection checks and returns the ordered report.
// services is the resolved list of service keys to probe (see ParseServices).
func Run(ctx context.Context, cfg config.OnClusterConfig, cluster ClusterInfo, services []string) *Report {
	r := &Report{}

	// 1. Config / dialer.
	dialer, derr := emrconn.New(cfg)
	switch {
	case errors.Is(derr, emrconn.ErrDisabled):
		r.fail("on-cluster config", "off (emr.onCluster.mode unset)",
			"Set emr.onCluster.mode to socks, tunnel, or direct in config.yaml. Run 'aws_explorer emr hbase --help' for a worked setup.")
	case derr != nil:
		r.fail("on-cluster config", derr.Error(),
			"Fix emr.onCluster in config.yaml — the chosen mode's required fields are missing or invalid.")
	default:
		r.ok("on-cluster config", configDetail(cfg, dialer))
	}
	if dialer != nil {
		defer dialer.Close()
	}

	// 2. Cluster (pure AWS — worth reporting regardless of on-cluster config).
	switch {
	case cluster.DescribeErr != nil:
		r.fail("cluster", "DescribeCluster failed: "+trimErr(cluster.DescribeErr),
			"Check the cluster id and --region, and that your credentials can call elasticmapreduce:DescribeCluster.")
	case cluster.PrimaryDNS == "":
		r.fail("cluster", clusterStateDetail(cluster.State)+" — no primary-node DNS",
			"The cluster must be running with a primary node; a terminated cluster has no daemons to reach.")
	case !clusterUsable(cluster.State):
		r.warn("cluster", clusterStateDetail(cluster.State)+" — primary "+cluster.PrimaryDNS,
			"The cluster is not in a RUNNING/WAITING state, so its daemons may be unavailable.")
	default:
		r.ok("cluster", clusterStateDetail(cluster.State)+" — primary "+cluster.PrimaryDNS)
	}

	// Without a dialer the bridge and daemons cannot be probed at all.
	if dialer == nil {
		r.skip("bridge", "skipped — on-cluster access is not configured")
		for _, s := range services {
			r.skip(targets[s].label, "skipped — on-cluster access is not configured")
		}
		return r
	}

	host := cluster.PrimaryDNS

	// 3. Bridge — the layer between this machine and the cluster.
	bridgeOK := true
	switch dialer.Mode() {
	case emrconn.ModeDirect:
		r.ok("bridge (direct)", "no bridge — dialing the primary node directly (must be inside the VPC)")
	case emrconn.ModeSocks:
		if err := dialer.Bridge(ctx, host); err != nil {
			bridgeOK = false
			r.fail("bridge (socks)", "SOCKS proxy "+dialer.SocksProxy()+" not reachable",
				"No SSH dynamic tunnel is running. In a separate terminal run: ssh -i <key.pem> -N -D <port> hadoop@"+dnsOrPlaceholder(host)+
					" — and confirm emr.onCluster.socksProxy matches that -D port.")
		} else {
			r.ok("bridge (socks)", "SOCKS proxy "+dialer.SocksProxy()+" reachable")
		}
	case emrconn.ModeTunnel:
		if host == "" {
			bridgeOK = false
			r.skip("bridge (tunnel)", "skipped — primary-node DNS unknown")
		} else if err := dialer.Bridge(ctx, host); err != nil {
			bridgeOK = false
			detail, hint := tunnelFailure(err, host)
			r.fail("bridge (tunnel)", detail, hint)
		} else {
			r.ok("bridge (tunnel)", "SSH to "+host+" established")
		}
	}

	// 4. Per-service: port reachability, then (REST services) a health GET.
	if !bridgeOK || host == "" {
		for _, s := range services {
			r.skip(targets[s].label, "skipped — bridge not available")
		}
		return r
	}
	for _, s := range services {
		probeService(ctx, r, dialer, host, targets[s])
	}
	return r
}

// probeService runs the port and (for REST daemons) protocol checks for one
// target, appending the outcome lines to the report.
func probeService(ctx context.Context, r *Report, d *emrconn.Dialer, host string, t target) {
	port := t.port
	if !t.portOnly {
		if p := d.Port(t.svc); p > 0 {
			port = p
		}
	}
	portName := fmt.Sprintf("%s port %d", t.label, port)

	conn, err := d.Dial(ctx, host, port)
	if err != nil {
		r.fail(portName, "not reachable",
			fmt.Sprintf("%s isn't listening, or the cluster security group / your tunnel doesn't allow port %d. Confirm the application is installed on this cluster and the daemon is running.", t.label, port))
		return
	}
	_ = conn.Close()

	if t.portOnly {
		r.warn(portName, "reachable (port-only check)",
			"HiveServer2 speaks Thrift, not HTTP, so only the TCP port was verified — not that the service is healthy. Use beeline to confirm a full connection.")
		return
	}
	r.ok(portName, "reachable")

	if _, err := d.GetRaw(ctx, t.svc, host, t.health); err != nil {
		r.fail(t.label+" daemon", "port open but "+t.health+" did not respond ("+trimErr(err)+")",
			fmt.Sprintf("The port is open but the %s daemon didn't answer its health endpoint — it may be starting, unhealthy, or on a non-default port (set emr.onCluster.ports). Check the daemon log on the primary node.", t.label))
		return
	}
	r.ok(t.label+" daemon", "healthy ("+t.health+")")
}

// --- pure helpers -------------------------------------------------------------

func configDetail(cfg config.OnClusterConfig, d *emrconn.Dialer) string {
	switch d.Mode() {
	case emrconn.ModeSocks:
		return "mode=socks · proxy " + d.SocksProxy()
	case emrconn.ModeTunnel:
		key := cfg.SSH.KeyFile
		return "mode=tunnel · ssh " + orDefault(cfg.SSH.User, "hadoop") + "@<primary> · key " + key
	case emrconn.ModeDirect:
		return "mode=direct (in-VPC)"
	default:
		return string(d.Mode())
	}
}

// clusterUsable reports whether a cluster state means its daemons could be up.
func clusterUsable(state string) bool {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "RUNNING", "WAITING":
		return true
	default:
		return false
	}
}

func clusterStateDetail(state string) string {
	if strings.TrimSpace(state) == "" {
		return "state unknown"
	}
	return strings.ToUpper(state)
}

func dnsOrPlaceholder(host string) string {
	if host == "" {
		return "<primary-dns>"
	}
	return host
}

// tunnelFailure classifies an SSH-bridge error into a detail line and an
// actionable hint, separating a network/SG block (can't reach port 22) from an
// authentication failure (wrong key/user) — the two have different fixes.
func tunnelFailure(err error, host string) (detail, hint string) {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unable to authenticate"),
		strings.Contains(msg, "handshake failed"),
		strings.Contains(msg, "no supported methods"),
		strings.Contains(msg, "parse ssh key"),
		strings.Contains(msg, "ssh key"):
		return "SSH to " + host + " failed authentication",
			"The SSH key or user is wrong. Set emr.onCluster.ssh.user (EMR default: hadoop) and ssh.keyFile to the cluster's key pair (an unencrypted private key)."
	case strings.Contains(msg, "timeout"),
		strings.Contains(msg, "timed out"),
		strings.Contains(msg, "deadline"),
		strings.Contains(msg, "no route to host"):
		return "SSH to " + host + " timed out",
			"Port 22 is not reachable from here. Check your VPN / Direct Connect, the cluster security group's inbound SSH rule, and that the primary node is up."
	case strings.Contains(msg, "refused"):
		return "SSH to " + host + " refused",
			"Port 22 is closed. Check the cluster security group allows SSH from your IP and that sshd is running on the primary node."
	default:
		return "SSH to " + host + " failed: " + trimErr(err),
			"Verify network reachability to port 22 and the emr.onCluster.ssh.user / ssh.keyFile settings."
	}
}

// trimErr drops the ErrUnreachable prefix so the surfaced message stays readable.
func trimErr(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimPrefix(err.Error(), emrconn.ErrUnreachable.Error()+": ")
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
