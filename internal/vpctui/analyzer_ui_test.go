package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderAnalyzerList(t *testing.T) {
	m := &Model{analyzerList: []NetInsightsAnalysis{
		{AnalysisID: "nia-1", Source: "eni-a", Destination: "eni-b", Protocol: "tcp", DestPort: 443, Status: "succeeded", PathFound: boolp(true), StartDate: "2026-06-09 10:00"},
		{AnalysisID: "nia-2", Source: "eni-c", Destination: "igw-1", Protocol: "tcp", Status: "succeeded", PathFound: boolp(false)},
	}}
	out := ansi.Strip(m.renderAnalyzerList())
	for _, want := range []string{"eni-a → eni-b:443", "[reachable]", "eni-c → igw-1", "[not reachable]", "2026-06-09 10:00"} {
		if !strings.Contains(out, want) {
			t.Errorf("analyzer list missing %q:\n%s", want, out)
		}
	}
}

func TestRenderAnalyzerListEmpty(t *testing.T) {
	m := &Model{}
	if !strings.Contains(ansi.Strip(m.renderAnalyzerList()), "No Reachability Analyzer analyses found") {
		t.Error("empty analyzer list should show a hint to create one")
	}
}

func TestViewAnalyzerConfirmWarnsAboutCost(t *testing.T) {
	m := &Model{width: 100, height: 30, showAnalyzer: true, analyzerConfirm: true,
		analyzerPendSrc: "eni-a", analyzerPendDst: "eni-b", analyzerPendPort: 443}
	m.analyzerVP = viewport.New(80, 20)
	out := ansi.Strip(m.viewAnalyzerOverlay("bg"))
	if !strings.Contains(out, "incurs a per-analysis charge") {
		t.Errorf("confirm step must warn about cost:\n%s", out)
	}
	if !strings.Contains(out, "eni-a → eni-b:443") {
		t.Errorf("confirm step should show the pending spec:\n%s", out)
	}
	if !strings.Contains(out, "y = create and run") {
		t.Errorf("confirm step should show the y/n prompt")
	}
}

func TestViewAnalyzerListMode(t *testing.T) {
	m := &Model{width: 100, height: 30, showAnalyzer: true, analyzerList: []NetInsightsAnalysis{
		{AnalysisID: "nia-1", Source: "eni-a", Destination: "eni-b", DestPort: 443, Status: "succeeded", PathFound: boolp(true)},
	}}
	m.analyzerVP = viewport.New(80, 20)
	m.analyzerVP.SetContent(m.renderAnalyzerList())
	out := ansi.Strip(m.viewAnalyzerOverlay("bg"))
	if !strings.Contains(out, "Reachability Analyzer") || !strings.Contains(out, "n new analysis (paid)") {
		t.Errorf("list mode should show the title and create hint:\n%s", out)
	}
}
