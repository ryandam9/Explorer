package vpctui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newCategoryModel builds a resource-browser model parked on the first
// selectable category, ready to receive an Enter keypress.
func newCategoryModel() (*Model, resourceType) {
	items := buildSidebarItems()
	firstIdx := firstSelectableIdx(items)
	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: firstIdx,
		sortCol:          -1,
		sortAsc:          true,
		state:            stateResourceBrowser,
		focus:            focusCategory,
		selectedVPC:      &VPCInfo{ID: "vpc-test"},
		width:            120,
		height:           40,
	}
	rt := items[firstIdx].rt
	m.initResourceTable(rt)
	return m, rt
}

// TestCategoryEnterFocusesResourceTable guards issue #248: pressing Enter on a
// resource type in the centre pane moves control to the resource table on the
// right with the cursor on the first row — but only once the load confirms the
// type actually has resources (issue #298).
func TestCategoryEnterFocusesResourceTable(t *testing.T) {
	m, rt := newCategoryModel()

	// Enter records the intent to jump but keeps control in the category pane
	// while the (uncached) load is in flight.
	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.focus != focusCategory {
		t.Errorf("focus = %v, want focusCategory while the load is in flight", m.focus)
	}
	if !m.pendingResourceFocus {
		t.Error("Enter should record a pending focus jump")
	}
	if m.activeResource != rt {
		t.Errorf("activeResource = %v, want %v", m.activeResource, rt)
	}

	// When resources arrive, control jumps to the table and the cursor sits on
	// the first row.
	m.Update(resourcesLoadedMsg{vpcID: "vpc-test", rt: rt, maps: []map[string]string{
		{"id": "r-1"},
		{"id": "r-2"},
	}})
	if m.focus != focusResourceTable {
		t.Errorf("focus = %v, want focusResourceTable after resources arrive", m.focus)
	}
	if !m.resourceTable.Focused() {
		t.Error("the resource table should be focused after resources arrive")
	}
	if m.pendingResourceFocus {
		t.Error("the pending focus flag should be cleared once it fires")
	}
	if got := m.resourceTable.Cursor(); got != 0 {
		t.Errorf("resource-table cursor = %d, want 0 (first resource)", got)
	}
}

// TestCategoryEnterEmptyStaysInCategory guards issue #298: pressing Enter on a
// resource type that turns out to have no resources keeps control in the
// category pane.
func TestCategoryEnterEmptyStaysInCategory(t *testing.T) {
	m, rt := newCategoryModel()

	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	// The empty load completes: no resources, so control stays put.
	m.Update(resourcesLoadedMsg{vpcID: "vpc-test", rt: rt, maps: nil})
	if m.focus != focusCategory {
		t.Errorf("focus = %v, want focusCategory for an empty resource type", m.focus)
	}
	if m.resourceTable.Focused() {
		t.Error("the resource table should not be focused for an empty type")
	}
	if m.pendingResourceFocus {
		t.Error("the pending focus flag should be cleared even when empty")
	}
}

// TestCategoryEnterCachedEmptyStaysInCategory covers the cached path of issue
// #298: when the count is already known to be zero, Enter must not jump.
func TestCategoryEnterCachedEmptyStaysInCategory(t *testing.T) {
	m, rt := newCategoryModel()
	m.resourceMaps[rt] = []map[string]string{} // cached, empty

	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.focus != focusCategory {
		t.Errorf("focus = %v, want focusCategory for a cached-empty type", m.focus)
	}
	if m.resourceTable.Focused() {
		t.Error("the resource table should not be focused for a cached-empty type")
	}
}

// TestCategoryEnterCachedNonEmptyJumps covers the cached non-empty path: the
// count is already known, so Enter jumps immediately without waiting on a load.
func TestCategoryEnterCachedNonEmptyJumps(t *testing.T) {
	m, rt := newCategoryModel()
	m.resourceMaps[rt] = []map[string]string{{"id": "r-1"}}

	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.focus != focusResourceTable {
		t.Errorf("focus = %v, want focusResourceTable for a cached non-empty type", m.focus)
	}
	if !m.resourceTable.Focused() {
		t.Error("the resource table should be focused for a cached non-empty type")
	}
}

// TestCategoryEnterOnHeaderStays verifies Enter on a non-selectable header is a
// no-op (it neither changes focus nor a resource).
func TestCategoryEnterOnHeaderStays(t *testing.T) {
	items := buildSidebarItems()
	headerIdx := -1
	for i, it := range items {
		if it.isHeader {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		t.Skip("no header item in the sidebar")
	}
	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: headerIdx,
		state:            stateResourceBrowser,
		focus:            focusCategory,
		selectedVPC:      &VPCInfo{ID: "vpc-test"},
		width:            120,
		height:           40,
	}
	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.focus != focusCategory {
		t.Errorf("focus = %v, want it to stay on focusCategory for a header", m.focus)
	}
}
