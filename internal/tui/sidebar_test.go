package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Regression test for issue #72: sidebar service names of varying lengths
// must render as uniform, left-aligned, single-line rows — over-wide names
// are truncated, never wrapped onto a second line.
func TestSidebarServiceNamesUniformSingleLine(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	resources := []model.Resource{
		{Service: "elasticloadbalancing", Type: "loadbalancer", Region: "us-east-1", ID: "lb-1", Name: "lb", State: "active"},
		{Service: "secretsmanager", Type: "secret", Region: "us-east-1", ID: "sec-1", Name: "s", State: "active"},
		{Service: "kms", Type: "key", Region: "us-east-1", ID: "key-1", Name: "k", State: "active"},
		{Service: "emr", Type: "cluster", Region: "us-east-1", ID: "j-1", Name: "c", State: "running"},
	}
	m = update(m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: resources}})

	sidebar := m.renderSidebar()
	plain := ansi.Strip(sidebar)

	// An over-wide name is truncated to the row budget…
	if strings.Contains(plain, "elasticloadbalancing") {
		t.Error("over-wide service name should be truncated, not rendered in full")
	}
	if !strings.Contains(plain, "elasticloadba…") {
		t.Errorf("expected the truncated name, sidebar:\n%s", plain)
	}
	// …while a name that exactly fills the budget fits untruncated.
	if !strings.Contains(plain, "secretsmanager") {
		t.Errorf("a name that fits the row budget should not be truncated, sidebar:\n%s", plain)
	}

	lines := strings.Split(plain, "\n")

	// Same width: every panel line renders at one display width. A wrapped
	// row would break this and shift every row below it.
	for i, ln := range lines {
		if w := ansi.StringWidth(ln); w != ansi.StringWidth(lines[0]) {
			t.Errorf("line %d width %d != %d — sidebar rows are not uniform:\n%s",
				i, w, ansi.StringWidth(lines[0]), plain)
		}
	}

	// One line per service: counting the non-empty content rows (excluding
	// borders and the panel title) must match the service list exactly.
	rows := 0
	for _, ln := range lines {
		s := strings.Trim(ln, "│ \t")
		if s == "" || strings.ContainsRune(s, '─') || s == "Services" {
			continue
		}
		rows++
	}
	if rows != len(m.services) {
		t.Errorf("expected %d single-line service rows, got %d:\n%s", len(m.services), rows, plain)
	}
}
