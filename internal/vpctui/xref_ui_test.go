package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderXref(t *testing.T) {
	m := &Model{xrefTitle: "sg-web", xrefGroups: crossReference(xrefSnap(), "sg-web")}
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

func TestViewXrefOverlay(t *testing.T) {
	m := &Model{width: 100, height: 30, xrefTitle: "subnet-priv", xrefGroups: crossReference(xrefSnap(), "subnet-priv")}
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
