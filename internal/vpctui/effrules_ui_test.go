package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderEffRules(t *testing.T) {
	m := &Model{effRules: computeEffectiveRules(effSnap(), "eni-app")}
	out := ansi.Strip(m.renderEffRules())
	for _, want := range []string{
		"Security groups: sg-a, sg-b",
		"Inbound",
		"HTTPS (TCP 443)",
		"via sg-a, sg-b", // merged contribution
		"Outbound",
		"acl-1 also applies",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("effective-rules render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderEffRulesNotFound(t *testing.T) {
	m := &Model{effRules: effectiveRuleSet{ENIID: "eni-x"}}
	out := ansi.Strip(m.renderEffRules())
	if !strings.Contains(out, "not found") {
		t.Errorf("expected not-found message, got: %s", out)
	}
}

func TestViewEffRulesOverlay(t *testing.T) {
	m := &Model{width: 100, height: 30, effRules: computeEffectiveRules(effSnap(), "eni-app")}
	m.effRulesVP = viewport.New(80, 20)
	m.effRulesVP.SetContent(m.renderEffRules())
	out := ansi.Strip(m.viewEffRulesOverlay("bg"))
	if !strings.Contains(out, "Effective rules: eni-app") {
		t.Errorf("overlay should show the title, got:\n%s", out)
	}
}
