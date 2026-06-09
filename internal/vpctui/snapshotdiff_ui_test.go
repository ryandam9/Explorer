package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/x/ansi"
)

func diffChanges() []snapshotChange {
	old := diffBase()
	neu := diffBase()
	neu.SecurityGroups = []SGInfo{
		{ID: "sg-web", Rules: []SGRule{
			{Direction: "Inbound", Protocol: "TCP", PortRange: "443", Source: "0.0.0.0/0"},
			{Direction: "Inbound", Protocol: "TCP", PortRange: "22", Source: "10.0.0.0/8"},
		}},
		{ID: "sg-new"},
	}
	return diffSnapshots(old, neu)
}

func TestRenderDiff(t *testing.T) {
	m := &Model{snapDiff: diffChanges()}
	out := ansi.Strip(m.renderDiff())
	for _, want := range []string{"+ Security group sg-new", "- Security group sg-old", "~ Security group sg-web", "+ inbound|tcp|22"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDiffNoChanges(t *testing.T) {
	m := &Model{}
	out := ansi.Strip(m.renderDiff())
	if !strings.Contains(out, "No changes since the baseline") {
		t.Errorf("empty diff should show a clean message, got: %s", out)
	}
}

func TestViewDiffOverlayCounts(t *testing.T) {
	m := &Model{width: 100, height: 30, snapDiff: diffChanges()}
	m.diffVP = viewport.New(80, 20)
	m.diffVP.SetContent(m.renderDiff())
	out := ansi.Strip(m.viewDiffOverlay("bg"))
	if !strings.Contains(out, "1 added, 1 removed, 1 modified") {
		t.Errorf("overlay should summarize counts, got:\n%s", out)
	}
	if !strings.Contains(out, "new baseline") {
		t.Errorf("overlay should advertise re-baselining")
	}
}
