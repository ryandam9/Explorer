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
		// A name wider than the sidebar's row budget, to exercise truncation.
		{Service: "elasticloadbalancingsupername", Type: "loadbalancer", Region: "us-east-1", ID: "lb-1", Name: "lb", State: "active"},
		{Service: "secretsmanager", Type: "secret", Region: "us-east-1", ID: "sec-1", Name: "s", State: "active"},
		{Service: "kms", Type: "key", Region: "us-east-1", ID: "key-1", Name: "k", State: "active"},
		{Service: "emr", Type: "cluster", Region: "us-east-1", ID: "j-1", Name: "c", State: "running"},
	}
	m = update(m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: resources}})

	sidebar := m.renderSidebar()
	plain := ansi.Strip(sidebar)

	// An over-wide name is truncated to the row budget with an ellipsis…
	if strings.Contains(plain, "elasticloadbalancingsupername") {
		t.Error("over-wide service name should be truncated, not rendered in full")
	}
	if !strings.Contains(plain, "…") {
		t.Errorf("expected a truncation ellipsis on the over-wide name, sidebar:\n%s", plain)
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

// The sidebar shows a live per-service resource count to the right of each
// name, plus the aggregate next to "All".
func TestSidebarShowsResourceCounts(t *testing.T) {
	m := newTestModel(t, 120, 40) // seeds: s3 x1, ec2 x2

	plain := ansi.Strip(m.renderSidebar())
	lineFor := func(name string) string {
		for _, ln := range strings.Split(plain, "\n") {
			if strings.Contains(ln, name) {
				return ln
			}
		}
		return ""
	}

	if ln := lineFor("ec2"); !strings.Contains(ln, "2") {
		t.Errorf("ec2 row should show its count of 2, got %q\n%s", ln, plain)
	}
	if ln := lineFor("s3"); !strings.Contains(ln, "1") {
		t.Errorf("s3 row should show its count of 1, got %q\n%s", ln, plain)
	}
	if ln := lineFor("All"); !strings.Contains(ln, "3") {
		t.Errorf("All row should show the aggregate count of 3, got %q\n%s", ln, plain)
	}
}
