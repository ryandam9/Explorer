// Package emrconn is the opt-in on-cluster connection layer for the EMR
// dashboard (AXE-039). The live YARN / HBase / Oozie services have no AWS API —
// they run as REST daemons on a cluster's primary node, reachable only from
// inside the cluster's VPC. This package is the single, deliberately fenced-off
// place where the tool reaches outside the AWS API surface to GET those
// endpoints, and it is OFF by default.
//
// All requests are read-only HTTP GETs with a bounded timeout. An unreachable
// daemon returns ErrUnreachable so the dashboard can render a "how to connect"
// helper instead of a crash.
package emrconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"golang.org/x/net/proxy"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// Mode is how the daemons are reached.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeDirect Mode = "direct"
	ModeSocks  Mode = "socks"
)

// Default daemon ports on Amazon EMR.
const (
	DefaultYARNPort  = 8088
	DefaultHBasePort = 8080  // HBase REST server
	DefaultOoziePort = 11000 // Oozie REST/web
	DefaultTimeout   = 5 * time.Second
)

// ErrUnreachable wraps any failure to reach a daemon (mode off, dial refused,
// timeout, …) so callers can render the connect helper rather than an error.
var ErrUnreachable = errors.New("on-cluster daemon unreachable")

// ErrDisabled is the specific ErrUnreachable case where the user hasn't opted
// in (mode "off").
var ErrDisabled = fmt.Errorf("%w: on-cluster access is off", ErrUnreachable)

// Service identifies which daemon a request targets, selecting its default port.
type Service string

const (
	ServiceYARN  Service = "yarn"
	ServiceHBase Service = "hbase"
	ServiceOozie Service = "oozie"
)

// Dialer reaches a cluster's on-cluster daemons per the resolved configuration.
type Dialer struct {
	mode    Mode
	ports   config.OnClusterPorts
	timeout time.Duration
	client  *http.Client
}

// New builds a Dialer from the on-cluster config. It returns (nil, ErrDisabled)
// when the mode is off or unset, so the live browsers stay dark until opted in.
func New(cfg config.OnClusterConfig) (*Dialer, error) {
	mode := normalizeMode(cfg.Mode)
	if mode == ModeOff {
		return nil, ErrDisabled
	}

	timeout := DefaultTimeout
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}

	transport := &http.Transport{}
	switch mode {
	case ModeDirect:
		// Default transport dials directly; nothing to configure.
	case ModeSocks:
		if cfg.SocksProxy == "" {
			return nil, fmt.Errorf("socks mode requires emr.onCluster.socksProxy (e.g. 127.0.0.1:8157)")
		}
		sd, err := proxy.SOCKS5("tcp", cfg.SocksProxy, nil, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("invalid socks proxy %q: %w", cfg.SocksProxy, err)
		}
		cd, ok := sd.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("socks dialer does not support contexts")
		}
		transport.DialContext = cd.DialContext
	default:
		return nil, fmt.Errorf("unknown on-cluster mode %q (want off|direct|socks)", cfg.Mode)
	}

	return &Dialer{
		mode:    mode,
		ports:   cfg.Ports,
		timeout: timeout,
		client:  &http.Client{Transport: transport, Timeout: timeout},
	}, nil
}

// Mode returns the dialer's resolved mode.
func (d *Dialer) Mode() Mode { return d.mode }

// Port returns the configured (or default) port for a service.
func (d *Dialer) Port(svc Service) int {
	switch svc {
	case ServiceYARN:
		return orDefault(d.ports.YARN, DefaultYARNPort)
	case ServiceHBase:
		return orDefault(d.ports.HBase, DefaultHBasePort)
	case ServiceOozie:
		return orDefault(d.ports.Oozie, DefaultOoziePort)
	default:
		return 0
	}
}

// BaseURL builds the http://<host>:<port> base for a service on a primary node.
func (d *Dialer) BaseURL(svc Service, host string) string {
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, fmt.Sprintf("%d", d.Port(svc))))
}

// GetJSON GETs path against the service on host and decodes the JSON body into
// v. A dial/timeout/HTTP error is wrapped in ErrUnreachable so the caller shows
// the connect helper.
func (d *Dialer) GetJSON(ctx context.Context, svc Service, host, path string, v any) error {
	body, err := d.GetRaw(ctx, svc, host, path)
	if err != nil {
		return err
	}
	return decodeJSON(body, v)
}

// GetRaw GETs path against the service on host and returns the raw body,
// wrapping transport/HTTP failures in ErrUnreachable.
func (d *Dialer) GetRaw(ctx context.Context, svc Service, host, path string) ([]byte, error) {
	return d.get(ctx, d.BaseURL(svc, host)+path)
}

// get GETs a URL and returns the raw body, wrapping transport failures in
// ErrUnreachable.
func (d *Dialer) get(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%w: %s returned HTTP %d", ErrUnreachable, url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func orDefault(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func normalizeMode(s string) Mode {
	switch Mode(toLowerTrim(s)) {
	case ModeDirect:
		return ModeDirect
	case ModeSocks:
		return ModeSocks
	default:
		return ModeOff
	}
}
