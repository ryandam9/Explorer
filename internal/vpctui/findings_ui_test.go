package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func sampleFindings() []Finding {
	return []Finding{
		{Severity: SevCritical, Resource: "sg-1", Title: "SG exposes a sensitive port", Detail: "sg-1 allows SSH from 0.0.0.0/0", Fix: "Restrict the source."},
		{Severity: SevWarning, Resource: "nat-1", Title: "NAT gateway is not referenced", Detail: "nat-1 is idle", Fix: "Delete it."},
		{Severity: SevInfo, Resource: "subnet-1", Title: "Subnet has no outbound internet path", Detail: "subnet-1 is isolated"},
	}
}

func TestRenderFindings(t *testing.T) {
	m := &Model{findings: sampleFindings()}
	out := ansi.Strip(m.renderFindings())

	for _, want := range []string{
		"CRITICAL", "WARNING", "INFO",
		"SG exposes a sensitive port", "[sg-1]",
		"NAT gateway is not referenced", "[nat-1]",
		"Subnet has no outbound internet path", "[subnet-1]",
		"Fix: Restrict the source.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("renderFindings output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderFindingsEmpty(t *testing.T) {
	m := &Model{}
	out := ansi.Strip(m.renderFindings())
	if !strings.Contains(out, "No issues detected") {
		t.Errorf("empty findings should render a clean-bill message, got: %s", out)
	}
}

func TestViewFindingsOverlayCounts(t *testing.T) {
	m := &Model{width: 120, height: 40, findings: sampleFindings()}
	m.findingsViewport = viewport.New(80, 20)
	m.findingsViewport.SetContent(m.renderFindings())

	out := ansi.Strip(m.viewFindingsOverlay("bg"))
	if !strings.Contains(out, "VPC findings — 1 critical, 1 warning, 1 info") {
		t.Errorf("overlay title should summarize counts, got:\n%s", out)
	}
	if !strings.Contains(out, "Esc/F close") {
		t.Errorf("overlay should show the close hint")
	}
}

func TestViewFindingsOverlayError(t *testing.T) {
	m := &Model{width: 120, height: 40, findingsErr: errSentinel{}}
	m.findingsViewport = viewport.New(80, 20)
	out := ansi.Strip(m.viewFindingsOverlay("bg"))
	if !strings.Contains(out, "Error: boom") {
		t.Errorf("overlay should surface the error, got:\n%s", out)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "boom" }
