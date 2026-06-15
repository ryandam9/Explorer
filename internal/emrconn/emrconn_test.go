package emrconn

import (
	"strings"
	"testing"

	"github.com/ryandam9/aws_explorer/internal/config"
)

func TestNew_OffByDefault(t *testing.T) {
	for _, mode := range []string{"", "off", "OFF", "  off  "} {
		d, err := New(config.OnClusterConfig{Mode: mode})
		if d != nil {
			t.Errorf("mode %q: expected nil dialer", mode)
		}
		if !IsUnreachable(err) {
			t.Errorf("mode %q: expected ErrUnreachable, got %v", mode, err)
		}
	}
}

func TestNew_DirectMode(t *testing.T) {
	d, err := New(config.OnClusterConfig{Mode: "direct"})
	if err != nil {
		t.Fatalf("direct mode: %v", err)
	}
	if d.Mode() != ModeDirect {
		t.Errorf("mode = %q, want direct", d.Mode())
	}
}

func TestNew_SocksRequiresProxy(t *testing.T) {
	if _, err := New(config.OnClusterConfig{Mode: "socks"}); err == nil {
		t.Error("socks mode without socksProxy should error")
	}
	d, err := New(config.OnClusterConfig{Mode: "socks", SocksProxy: "127.0.0.1:8157"})
	if err != nil {
		t.Fatalf("socks mode: %v", err)
	}
	if d.Mode() != ModeSocks {
		t.Errorf("mode = %q, want socks", d.Mode())
	}
}

func TestPortsAndBaseURL(t *testing.T) {
	d, _ := New(config.OnClusterConfig{Mode: "direct"})
	if got := d.Port(ServiceYARN); got != DefaultYARNPort {
		t.Errorf("default yarn port = %d, want %d", got, DefaultYARNPort)
	}
	if got := d.Port(ServiceHBase); got != DefaultHBasePort {
		t.Errorf("default hbase port = %d, want %d", got, DefaultHBasePort)
	}
	if got := d.BaseURL(ServiceYARN, "ip-10-0-0-1.ec2.internal"); got != "http://ip-10-0-0-1.ec2.internal:8088" {
		t.Errorf("BaseURL = %q", got)
	}

	// Overrides.
	d2, _ := New(config.OnClusterConfig{Mode: "direct", Ports: config.OnClusterPorts{YARN: 9999}})
	if got := d2.Port(ServiceYARN); got != 9999 {
		t.Errorf("overridden yarn port = %d, want 9999", got)
	}
}

func TestConnectHelp(t *testing.T) {
	help := ConnectHelp("ip-10-0-0-1.ec2.internal", 8088)
	for _, want := range []string{"onCluster", "ssh -i", "ip-10-0-0-1.ec2.internal", "8088"} {
		if !strings.Contains(help, want) {
			t.Errorf("connect help missing %q", want)
		}
	}
	// No DNS → no tunnel command.
	help = ConnectHelp("", 0)
	if strings.Contains(help, "ssh -i") {
		t.Error("no DNS should not produce an ssh command")
	}
}

func TestNew_TunnelRequiresSSH(t *testing.T) {
	// Missing user/key → error.
	if _, err := New(config.OnClusterConfig{Mode: "tunnel"}); err == nil {
		t.Error("tunnel mode without ssh settings should error")
	}
	// Nonexistent key file → error from key load.
	_, err := New(config.OnClusterConfig{Mode: "tunnel", SSH: config.OnClusterSSH{User: "hadoop", KeyFile: "/no/such/key.pem"}})
	if err == nil {
		t.Error("tunnel mode with a missing key file should error")
	}
}

func TestTunnelModeRecognized(t *testing.T) {
	if normalizeMode("tunnel") != ModeTunnel || normalizeMode(" TUNNEL ") != ModeTunnel {
		t.Error("tunnel mode not recognized")
	}
}

func TestPathOf(t *testing.T) {
	cases := map[string]string{
		"http://host:8080/orders/scanner/123":     "/orders/scanner/123",
		"http://host:8080/orders/scanner/123?x=1": "/orders/scanner/123?x=1",
		"/already/a/path":                         "/already/a/path",
	}
	for in, want := range cases {
		if got := PathOf(in); got != want {
			t.Errorf("PathOf(%q) = %q, want %q", in, got, want)
		}
	}
}
