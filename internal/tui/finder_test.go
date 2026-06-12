package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/model"
)

func ctrlP() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyCtrlP} }

func typeRunes(t *testing.T, m tuiModel, s string) tuiModel {
	t.Helper()
	for _, r := range s {
		m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestFinderOpensAndLists(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, ctrlP())
	if !m.showFinder {
		t.Fatal("ctrl+p should open the finder")
	}
	// Empty query lists everything, capped.
	if len(m.finderHits) != len(m.sorted) {
		t.Errorf("empty query hits = %d, want %d", len(m.finderHits), len(m.sorted))
	}
	plain := ansi.Strip(m.View())
	for _, want := range []string{"Jump to resource", "Enter jump", "web-1"} {
		if !strings.Contains(plain, want) {
			t.Errorf("finder view missing %q", want)
		}
	}
	// Status bar advertises the finder keys.
	if !strings.Contains(plain, "jump") {
		t.Error("status bar should advertise the finder")
	}
}

func TestFinderFiltersAndNavigates(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, ctrlP())
	m = typeRunes(t, m, "web")
	if len(m.finderHits) != 2 {
		t.Fatalf("hits for 'web' = %d, want 2 (web-1, web-2)", len(m.finderHits))
	}
	if m.finderSel != 0 {
		t.Errorf("selection should start at 0, got %d", m.finderSel)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.finderSel != 1 {
		t.Errorf("down should move selection to 1, got %d", m.finderSel)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.finderSel != 1 {
		t.Errorf("selection should clamp at the last hit, got %d", m.finderSel)
	}
	m = update(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.finderSel != 0 {
		t.Errorf("up should move selection back to 0, got %d", m.finderSel)
	}
}

func TestFinderEscCloses(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, ctrlP())
	m = typeRunes(t, m, "web")
	m = update(m, key("esc"))
	if m.showFinder {
		t.Fatal("esc should close the finder")
	}
	if m.showDetail {
		t.Error("esc must not open any detail panel")
	}
}

func TestFinderJumpOpensDetail(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, ctrlP())
	m = typeRunes(t, m, "i-def456") // the eu-west-1 instance
	if len(m.finderHits) == 0 {
		t.Fatal("expected a hit for i-def456")
	}
	m = update(m, key("enter"))

	if m.showFinder {
		t.Error("enter should close the finder")
	}
	if !m.showDetail || m.detail == nil {
		t.Fatal("enter should open the detail panel for the hit")
	}
	if m.detail.ID != "i-def456" {
		t.Errorf("detail = %q, want i-def456", m.detail.ID)
	}
	if m.focus != focusDetail {
		t.Errorf("focus = %v, want detail", m.focus)
	}
	// The sidebar selected the resource's service and the cursor is on its row.
	if got := m.currentService(); got != "ec2" {
		t.Errorf("currentService = %q, want ec2", got)
	}
	if res, ok := m.selectedResource(); !ok || res.ID != "i-def456" {
		t.Errorf("selectedResource = %+v ok=%v, want i-def456", res, ok)
	}
}

func TestFinderJumpClearsHidingFilters(t *testing.T) {
	m := newTestModel(t, 140, 40)
	// Apply a quick filter that hides the instances.
	m = update(m, key("/"))
	m = typeRunes(t, m, "logs")
	m = update(m, key("enter"))

	m = update(m, ctrlP())
	m = typeRunes(t, m, "i-abc123")
	m = update(m, key("enter"))

	if m.filterText != "" {
		t.Errorf("jump should clear the quick filter, still %q", m.filterText)
	}
	if res, ok := m.selectedResource(); !ok || res.ID != "i-abc123" {
		t.Errorf("selectedResource = %+v ok=%v, want i-abc123", res, ok)
	}
}

func TestFinderNoMatches(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, ctrlP())
	m = typeRunes(t, m, "zzzznothing")
	if len(m.finderHits) != 0 {
		t.Fatalf("hits = %d, want 0", len(m.finderHits))
	}
	if !strings.Contains(ansi.Strip(m.View()), "no resources match") {
		t.Error("view should say nothing matches")
	}
	// Enter with no hits just closes.
	m = update(m, key("enter"))
	if m.showFinder || m.showDetail {
		t.Error("enter with no hits should close without opening detail")
	}
}

func TestSameResource(t *testing.T) {
	a := model.Resource{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1",
		ARN: "arn:aws:ec2:us-east-1:1:instance/i-1"}
	b := a
	if !sameResource(a, b) {
		t.Error("identical ARNs should match")
	}
	b.ARN = "arn:other"
	if sameResource(a, b) {
		t.Error("different ARNs should not match")
	}
	a.ARN, b.ARN = "", ""
	if !sameResource(a, b) {
		t.Error("same service/id/region without ARNs should match")
	}
	b.Region = "eu-west-1"
	if sameResource(a, b) {
		t.Error("different regions should not match")
	}
}
