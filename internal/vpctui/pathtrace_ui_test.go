package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestParseTraceTarget(t *testing.T) {
	cases := []struct {
		in   string
		ip   string
		port int
	}{
		{"10.0.1.20:3306", "10.0.1.20", 3306},
		{"internet:443", "internet", 443},
		{"8.8.8.8", "8.8.8.8", -1},
		{"  10.0.0.5 : 22 ", "10.0.0.5", 22},
	}
	for _, c := range cases {
		ip, port := parseTraceTarget(c.in)
		if ip != c.ip || port != c.port {
			t.Errorf("parseTraceTarget(%q) = (%q,%d), want (%q,%d)", c.in, ip, port, c.ip, c.port)
		}
	}
}

func TestRenderTraceResultReachable(t *testing.T) {
	m := &Model{traceResult: tracePath(baseSnap(), traceRequest{
		SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 3306,
	})}
	out := ansi.Strip(m.renderTraceResult())
	for _, want := range []string{"Reachable", "✓ Security group egress", "✓ Route table", "✓ Destination security group ingress"} {
		if !strings.Contains(out, want) {
			t.Errorf("trace render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderTraceResultBlocked(t *testing.T) {
	m := &Model{traceResult: tracePath(baseSnap(), traceRequest{
		SourceENIID: "eni-web", DestIP: "10.0.1.20", Protocol: "tcp", Port: 5432,
	})}
	out := ansi.Strip(m.renderTraceResult())
	if !strings.Contains(out, "Blocked at") || !strings.Contains(out, "✗") {
		t.Errorf("blocked trace should show a failed hop:\n%s", out)
	}
}

func TestViewTraceResultOverlayLoading(t *testing.T) {
	m := &Model{width: 100, height: 30, traceLoading: true}
	m.traceViewport = viewport.New(80, 20)
	out := ansi.Strip(m.viewTraceResultOverlay("bg"))
	if !strings.Contains(out, "Connectivity Trace") {
		t.Errorf("overlay should show its title while loading:\n%s", out)
	}
}
