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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/net/proxy"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// PathOf extracts the request path (with query) from an absolute URL, so a
// Location header returned by the daemon can be re-fetched through the dialer.
// A value that is already a path is returned unchanged.
func PathOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return raw
	}
	if u.RawQuery != "" {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path
}

// Mode is how the daemons are reached.
type Mode string

const (
	ModeOff    Mode = "off"
	ModeDirect Mode = "direct"
	ModeSocks  Mode = "socks"
	ModeTunnel Mode = "tunnel"
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
	mode       Mode
	socksProxy string // host:port of the SOCKS5 proxy (socks mode only)
	ports      config.OnClusterPorts
	timeout    time.Duration
	client     *http.Client
	tunnel     *tunnelDialer // non-nil only in tunnel mode
	// dial reaches host:port through whatever bridge the mode configures (direct
	// net dial, the SOCKS proxy, or the SSH tunnel). It backs both the HTTP
	// transport and the raw Dial probe used by connect-check.
	dial func(ctx context.Context, network, addr string) (net.Conn, error)
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
	var tun *tunnelDialer
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	socksProxy := ""
	switch mode {
	case ModeDirect:
		// Plain dial straight to the primary node (the tool is inside the VPC).
		dial = (&net.Dialer{Timeout: timeout}).DialContext
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
		dial = cd.DialContext
		socksProxy = cfg.SocksProxy
	case ModeTunnel:
		t, err := newTunnelDialer(cfg.SSH, timeout)
		if err != nil {
			return nil, err
		}
		tun = t
		dial = t.dialContext
	default:
		return nil, fmt.Errorf("unknown on-cluster mode %q (want off|direct|socks|tunnel)", cfg.Mode)
	}
	transport.DialContext = dial

	return &Dialer{
		mode:       mode,
		socksProxy: socksProxy,
		ports:      cfg.Ports,
		timeout:    timeout,
		client:     &http.Client{Transport: transport, Timeout: timeout},
		tunnel:     tun,
		dial:       dial,
	}, nil
}

// Close releases any resources held by the dialer (the SSH connections used by
// tunnel mode). Safe to call on any dialer.
func (d *Dialer) Close() {
	if d != nil && d.tunnel != nil {
		d.tunnel.Close()
	}
}

// Mode returns the dialer's resolved mode.
func (d *Dialer) Mode() Mode { return d.mode }

// SocksProxy returns the configured SOCKS proxy address (socks mode only, else "").
func (d *Dialer) SocksProxy() string { return d.socksProxy }

// Dial opens a raw TCP connection to host:port through the configured bridge
// (direct / SOCKS / SSH tunnel), bounded by the dialer timeout. It is the
// transport-level probe behind connect-check — it proves a daemon port is
// reachable without speaking the daemon's protocol (used for Hive, whose
// HiveServer2 is Thrift, not HTTP). The caller closes the returned conn.
func (d *Dialer) Dial(ctx context.Context, host string, port int) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	conn, err := d.dial(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return conn, nil
}

// Bridge verifies the connection bridge itself — the layer between this machine
// and the cluster — independent of any daemon. In socks mode it checks the SOCKS
// proxy is listening; in tunnel mode it opens (and authenticates) the SSH
// connection to host; in direct mode there is no bridge, so it is a no-op.
// Transport failures wrap ErrUnreachable so connect-check can render a hint.
func (d *Dialer) Bridge(ctx context.Context, host string) error {
	switch d.mode {
	case ModeDirect:
		return nil
	case ModeSocks:
		c, err := (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, "tcp", d.socksProxy)
		if err != nil {
			return fmt.Errorf("%w: SOCKS proxy %s: %v", ErrUnreachable, d.socksProxy, err)
		}
		_ = c.Close()
		return nil
	case ModeTunnel:
		if d.tunnel == nil {
			return fmt.Errorf("%w: tunnel not initialized", ErrUnreachable)
		}
		if err := d.tunnel.connect(ctx, host); err != nil {
			return fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		return nil
	default:
		return nil
	}
}

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

// Response is the result of a non-GET request: the status code, body, and the
// Location response header (set by POSTs that create a resource).
type Response struct {
	Status   int
	Body     []byte
	Location string
}

// Request performs method against a service endpoint and returns the status,
// body and Location header. Transport failures wrap ErrUnreachable; a >=400
// status is returned without error so callers can branch on it (e.g. 204 = done).
func (d *Dialer) Request(ctx context.Context, method string, svc Service, host, path string, body []byte, contentType string) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.BaseURL(svc, host)+path, rdr)
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return Response{Status: resp.StatusCode, Body: b, Location: resp.Header.Get("Location")}, nil
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
	case ModeTunnel:
		return ModeTunnel
	default:
		return ModeOff
	}
}
