package audittui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ryandam9/aws_explorer/internal/audit"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

func testFindings() []findings.Finding {
	return []findings.Finding{
		{ID: "COST-EBS-001", Severity: findings.SevWarning, Service: "ec2", Region: "us-east-1",
			Resource: "vol-0abc", Title: "Unattached EBS volume (gp2, 1024 GiB)",
			Detail: "still bills", Fix: "delete it", EstMonthlyUSD: 102.40},
		{ID: "COST-EIP-001", Severity: findings.SevWarning, Service: "ec2", Region: "eu-west-1",
			Resource: "eipalloc-1", Title: "Elastic IP not associated",
			Detail: "bills hourly", Fix: "release it", EstMonthlyUSD: 3.65},
		{ID: "COST-EBS-002", Severity: findings.SevInfo, Service: "ec2", Region: "us-east-1",
			Resource: "vol-0def", Title: "gp2 volume could be gp3 (500 GiB)",
			Detail: "gp3 cheaper", Fix: "migrate", EstMonthlyUSD: 10.00,
			ARN: "arn:aws:ec2:us-east-1:1:volume/vol-0def"},
	}
}

// newTestModel builds a sized model with the sample findings applied.
func newTestModel(t *testing.T) Model {
	t.Helper()
	ch := make(chan audit.CostChunk, 2)
	m := New([]string{"us-east-1", "eu-west-1"}, ch)

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)

	mm, _ = m.Update(chunkMsg{ok: true, chunk: audit.CostChunk{
		Region:   "us-east-1",
		Findings: testFindings(),
		Errors:   []model.ExploreError{{Service: "cloudwatch", Region: "us-east-1", Code: "AccessDenied", Message: "denied"}},
	}})
	m = mm.(Model)
	mm, _ = m.Update(chunkMsg{ok: false})
	return mm.(Model)
}

func key(s string) tea.KeyMsg {
	if len(s) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	t := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	return t
}

func TestChunksAccumulateAndScanEnds(t *testing.T) {
	m := newTestModel(t)
	if m.scanning {
		t.Error("scanning should be false after the channel closes")
	}
	if m.scanned != 1 {
		t.Errorf("scanned = %d, want 1", m.scanned)
	}
	if len(m.all) != 3 || len(m.visible) != 3 {
		t.Errorf("all/visible = %d/%d, want 3/3", len(m.all), len(m.visible))
	}
	if len(m.errs) != 1 {
		t.Errorf("errs = %d, want 1", len(m.errs))
	}
	// findings.Sort ranks the most expensive warning first.
	if m.visible[0].Resource != "vol-0abc" {
		t.Errorf("first row = %q, want vol-0abc", m.visible[0].Resource)
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	for _, want := range []string{
		"Cost audit",
		"3 finding(s)",
		"potential savings",
		"COST-EBS-001",
		"⚠ 1 error(s)",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestViewEmptyState(t *testing.T) {
	ch := make(chan audit.CostChunk)
	m := New([]string{"us-east-1"}, ch)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)
	mm, _ = m.Update(chunkMsg{ok: false})
	m = mm.(Model)
	if v := m.View(); !strings.Contains(v, "No cost waste found") {
		t.Error("empty state should celebrate a clean account")
	}
}

func TestQuickFilter(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(key("/"))
	m = mm.(Model)
	if !m.filtering {
		t.Fatal("/ should enter filter mode")
	}
	for _, r := range "elastic" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	if len(m.visible) != 1 || m.visible[0].Resource != "eipalloc-1" {
		t.Fatalf("filtered visible = %+v, want only eipalloc-1", m.visible)
	}
	// Esc clears the filter.
	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.filtering || len(m.visible) != 3 {
		t.Errorf("esc should clear the filter: filtering=%v visible=%d", m.filtering, len(m.visible))
	}
}

func TestSortCycleAndReverse(t *testing.T) {
	m := newTestModel(t)

	// First press: severity column (1; the "#" column 0 is skipped),
	// descending (worst first) — same top row.
	mm, _ := m.Update(key("s"))
	m = mm.(Model)
	if m.sortCol != 1 || m.sortAsc {
		t.Fatalf("sortCol/asc = %d/%v, want 1/false", m.sortCol, m.sortAsc)
	}

	// Cycle to ID column (2, ascending).
	mm, _ = m.Update(key("s"))
	m = mm.(Model)
	if m.sortCol != 2 || !m.sortAsc {
		t.Fatalf("sortCol/asc = %d/%v, want 2/true", m.sortCol, m.sortAsc)
	}
	if m.visible[0].ID != "COST-EBS-001" {
		t.Errorf("first by ID = %q", m.visible[0].ID)
	}

	mm, _ = m.Update(key("R"))
	m = mm.(Model)
	if m.visible[0].ID != "COST-EIP-001" {
		t.Errorf("first by ID desc = %q", m.visible[0].ID)
	}

	// r resets to the natural ranking.
	mm, _ = m.Update(key("r"))
	m = mm.(Model)
	if m.sortCol != -1 || m.visible[0].Resource != "vol-0abc" {
		t.Errorf("reset: sortCol=%d first=%q", m.sortCol, m.visible[0].Resource)
	}
}

func TestSequenceColumn(t *testing.T) {
	m := newTestModel(t)
	rows := m.tbl.Rows()
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for i, want := range []string{"1", "2", "3"} {
		if rows[i][0] != want {
			t.Errorf("row %d sequence cell = %q, want %q", i, rows[i][0], want)
		}
	}
	if columns[0].Title != "#" {
		t.Errorf("first column = %q, want the # sequence column", columns[0].Title)
	}
}

func TestDetailOverlay(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(key("enter"))
	m = mm.(Model)
	if m.overlay != overlayDetail {
		t.Fatal("enter should open the detail overlay")
	}
	v := m.View()
	for _, want := range []string{"COST-EBS-001", "Why", "Fix", "delete it", "$102.40"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail overlay missing %q", want)
		}
	}
	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.overlay != overlayNone {
		t.Error("esc should close the overlay")
	}
}

func TestErrorsOverlay(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(key("e"))
	m = mm.(Model)
	if m.overlay != overlayErrors {
		t.Fatal("e should open the errors overlay when errors exist")
	}
	if v := m.View(); !strings.Contains(v, "cloudwatch@us-east-1") {
		t.Error("errors overlay should list service@region")
	}

	// Without errors, e does nothing.
	m2 := newTestModel(t)
	m2.errs = nil
	mm, _ = m2.Update(key("e"))
	if mm.(Model).overlay != overlayNone {
		t.Error("e with no errors should be a no-op")
	}
}

func TestHelpOverlay(t *testing.T) {
	m := newTestModel(t)
	mm, _ := m.Update(key("?"))
	m = mm.(Model)
	if m.overlay != overlayHelp {
		t.Fatal("? should open help")
	}
	if v := m.View(); !strings.Contains(v, "Quick filter") {
		t.Error("help overlay should list the keys")
	}
}

func TestKeyHintsAreContextual(t *testing.T) {
	m := newTestModel(t)
	if !hasHint(m.keyHints(), "e") {
		t.Error("hints with errors should offer e")
	}
	if hasHint(m.keyHints(), "R") {
		t.Error("R should be hidden in natural sort order")
	}

	m.errs = nil
	mm, _ := m.Update(key("s"))
	m = mm.(Model)
	if hasHint(m.keyHints(), "e") {
		t.Error("hints without errors should not offer e")
	}
	if !hasHint(m.keyHints(), "R") {
		t.Error("hints with a column sort should offer R")
	}
}

func hasHint(hs []ui.KeyHint, k string) bool {
	for _, h := range hs {
		if h.Key == k {
			return true
		}
	}
	return false
}

func TestPageTitle(t *testing.T) {
	m := New(nil, nil)
	if m.PageTitle() != "Audit › Cost" {
		t.Errorf("PageTitle = %q", m.PageTitle())
	}
}

func TestSelectedFollowsCursorAndFilter(t *testing.T) {
	m := newTestModel(t)
	m.tbl.SetCursor(2)
	f := m.selected()
	if f == nil || f.Resource != "vol-0def" {
		t.Fatalf("selected = %+v, want vol-0def", f)
	}
	// Filtering rebuilds rows; the cursor is clamped into range.
	mm, _ := m.Update(key("/"))
	m = mm.(Model)
	for _, r := range "elastic" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(Model)
	}
	if f := m.selected(); f == nil || f.Resource != "eipalloc-1" {
		t.Fatalf("selected after filter = %+v, want eipalloc-1", f)
	}
}

func TestFilterFindings(t *testing.T) {
	fs := testFindings()
	if got := filterFindings(fs, ""); len(got) != 3 {
		t.Errorf("empty query should keep all, got %d", len(got))
	}
	if got := filterFindings(fs, "EU-WEST"); len(got) != 1 {
		t.Errorf("region match (case-insensitive) = %d, want 1", len(got))
	}
	if got := filterFindings(fs, "release it"); len(got) != 1 {
		t.Errorf("fix-text match = %d, want 1", len(got))
	}
	if got := filterFindings(fs, "nomatch-xyz"); len(got) != 0 {
		t.Errorf("no match = %d, want 0", len(got))
	}
}

func TestSortFindingsByEstimate(t *testing.T) {
	fs := testFindings()
	// EST/MO is column 6 now that the "#" column leads the table.
	sortFindings(fs, 6, false)
	if fs[0].EstMonthlyUSD != 102.40 || fs[2].EstMonthlyUSD != 3.65 {
		t.Errorf("desc by est = %v, %v, %v", fs[0].EstMonthlyUSD, fs[1].EstMonthlyUSD, fs[2].EstMonthlyUSD)
	}
	sortFindings(fs, 6, true)
	if fs[0].EstMonthlyUSD != 3.65 {
		t.Errorf("asc by est should start at 3.65, got %v", fs[0].EstMonthlyUSD)
	}
}
