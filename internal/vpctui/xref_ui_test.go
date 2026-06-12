package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderXref(t *testing.T) {
	groups, _ := crossReference(xrefSnap(), "sg-web")
	m := &Model{xrefTitle: "sg-web", xrefGroups: groups}
	out := ansi.Strip(m.renderXref())
	for _, want := range []string{
		"Attached to network interfaces", "eni-a", "i-web",
		"Referenced by other security groups", "sg-db",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("xref render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderXrefEmpty(t *testing.T) {
	m := &Model{}
	out := ansi.Strip(m.renderXref())
	if !strings.Contains(out, "No related resources") {
		t.Errorf("empty xref should show a clear message, got: %s", out)
	}
}

func TestRenderXrefUnsupported(t *testing.T) {
	m := &Model{xrefUnsupported: true}
	out := ansi.Strip(m.renderXref())
	if !strings.Contains(out, "not available for this resource type") {
		t.Errorf("unsupported xref should say so explicitly, got: %s", out)
	}
	if !strings.Contains(out, "security groups") || !strings.Contains(out, "peering") {
		t.Errorf("unsupported message should list the supported types, got: %s", out)
	}
}

func TestXrefSupportedCategories(t *testing.T) {
	for _, rt := range []resourceType{
		rtSecurityGroups, rtSubnets, rtRouteTables, rtNetworkInterfaces,
		rtNatGateways, rtInternetGateways, rtNetworkACLs, rtEndpoints, rtPeering,
	} {
		if !xrefSupported(rt) {
			t.Errorf("%s should support cross-reference", rtLabel(rt))
		}
	}
	for _, rt := range []resourceType{rtFlowLogs, rtEC2Instances, rtLambda, rtRDS, rtLoadBalancers} {
		if xrefSupported(rt) {
			t.Errorf("%s should not advertise cross-reference", rtLabel(rt))
		}
	}
}

func TestViewXrefOverlay(t *testing.T) {
	groups, _ := crossReference(xrefSnap(), "subnet-priv")
	m := &Model{width: 100, height: 30, xrefTitle: "subnet-priv", xrefGroups: groups}
	m.xrefViewport = viewport.New(80, 20)
	m.xrefViewport.SetContent(m.renderXref())
	out := ansi.Strip(m.viewXrefOverlay("bg"))
	if !strings.Contains(out, "Where used: subnet-priv") {
		t.Errorf("overlay should show the title, got:\n%s", out)
	}
	if !strings.Contains(out, "Esc/x close") {
		t.Errorf("overlay should show the close hint")
	}
}
