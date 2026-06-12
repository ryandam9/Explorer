package vpctui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newFilterTestModel() *Model {
	items := buildSidebarItems()
	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: firstSelectableIdx(items),
		activeResource:   rtSubnets,
		sortCol:          -1,
		sortAsc:          true,
		state:            stateResourceBrowser,
		focus:            focusResourceTable,
	}
	m.resourceFilter = textinput.New()
	m.vpcSearch = textinput.New()
	m.initResourceTable(rtSubnets)
	m.resourceMaps[rtSubnets] = []map[string]string{
		{"subnet_id": "subnet-aaa", "name": "alpha", "cidr": "10.0.1.0/24"},
		{"subnet_id": "subnet-bbb", "name": "beta", "cidr": "10.0.2.0/24"},
		{"subnet_id": "subnet-ccc", "name": "gamma", "cidr": "10.0.3.0/24"},
	}
	m.rebuildResourceTable()
	return m
}

func TestResourceTableQuickFilter(t *testing.T) {
	m := newFilterTestModel()
	if len(m.resourceTable.Rows()) != 3 {
		t.Fatalf("expected 3 rows before filtering, got %d", len(m.resourceTable.Rows()))
	}

	// "/" enters filter mode; typing narrows the rows live.
	m.handleKey(keyRunes("/"))
	if !m.inResourceFilter {
		t.Fatal("expected / to enter filter mode")
	}
	for _, ch := range "beta" {
		m.handleKey(keyRunes(string(ch)))
	}
	rows := m.resourceTable.Rows()
	if len(rows) != 1 || rows[0][1] != "beta" {
		t.Fatalf("expected the single beta row, got %v", rows)
	}
	if len(m.resourceView) != 1 || m.resourceView[0]["subnet_id"] != "subnet-bbb" {
		t.Fatalf("resourceView must mirror the filtered rows, got %v", m.resourceView)
	}
	// The cursor resolves the selection through the filtered view, not the
	// full cache — detail, copy, and xref depend on this.
	if got := m.selectedResourceID(); got != "subnet-bbb" {
		t.Fatalf("selectedResourceID = %q, want subnet-bbb", got)
	}

	// Enter keeps the filter applied but leaves input mode.
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.inResourceFilter {
		t.Fatal("enter should leave filter input mode")
	}
	if len(m.resourceTable.Rows()) != 1 {
		t.Fatal("enter should keep the filter applied")
	}

	// Re-opening with "/" and pressing Esc clears it.
	m.handleKey(keyRunes("/"))
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.inResourceFilter || m.resourceFilter.Value() != "" {
		t.Fatal("esc should clear the filter and leave input mode")
	}
	if len(m.resourceTable.Rows()) != 3 {
		t.Fatalf("expected all rows back after esc, got %d", len(m.resourceTable.Rows()))
	}
}

func TestResourceFilterSurvivesSortAndNumbersRows(t *testing.T) {
	m := newFilterTestModel()
	m.handleKey(keyRunes("/"))
	m.handleKey(keyRunes("a")) // matches alpha, beta and gamma
	m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})

	// Sorting rebuilds the table; the filter must stay applied and the
	// visible rows renumbered 1..n.
	m.handleKey(keyRunes("s"))
	rows := m.resourceTable.Rows()
	if len(rows) != 3 {
		t.Fatalf("filter lost on sort, got %d rows", len(rows))
	}
	if rows[0][0] != "1" || rows[0][1] != "alpha" {
		t.Fatalf("expected renumbered sorted rows, got %v", rows[0])
	}
}

func TestResourceFilterCapturesGlobalKeys(t *testing.T) {
	m := newFilterTestModel()
	m.handleKey(keyRunes("/"))

	// q / S / ? are global shortcuts everywhere else; while the filter input
	// is focused they must be typed into the query instead.
	for _, k := range []string{"q", "S", "?"} {
		m.handleKey(keyRunes(k))
	}
	if m.showSettings || m.showHelp {
		t.Fatal("global keys leaked out of the filter input")
	}
	if got := m.resourceFilter.Value(); got != "qS?" {
		t.Fatalf("expected the keys typed into the query, got %q", got)
	}
}

func TestVPCSearchCapturesGlobalKeys(t *testing.T) {
	m := newFilterTestModel()
	m.state = stateVPCList
	m.focus = focusVPCSearch
	m.inVPCSearch = true
	m.vpcSearch.Focus()

	for _, k := range []string{"q", "S", "?"} {
		m.handleKey(keyRunes(k))
	}
	if m.showSettings || m.showHelp {
		t.Fatal("global keys leaked out of the VPC search input")
	}
	if got := m.vpcSearch.Value(); got != "qS?" {
		t.Fatalf("expected the keys typed into the search, got %q", got)
	}
}
