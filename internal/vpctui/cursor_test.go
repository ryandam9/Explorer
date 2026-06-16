package vpctui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestCategoryEnterFocusesResourceTable guards issue #248: pressing Enter on a
// resource type in the centre pane must move control to the resource table on
// the right, with the cursor on the first row.
func TestCategoryEnterFocusesResourceTable(t *testing.T) {
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

	// Enter on the selected category moves control to the resource table.
	m.handleCategoryKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.focus != focusResourceTable {
		t.Errorf("focus = %v, want focusResourceTable after Enter", m.focus)
	}
	if !m.resourceTable.Focused() {
		t.Error("the resource table should be focused after Enter on a category")
	}
	if m.activeResource != rt {
		t.Errorf("activeResource = %v, want %v", m.activeResource, rt)
	}

	// When the resources arrive, the cursor sits on the first row.
	m.Update(resourcesLoadedMsg{vpcID: "vpc-test", rt: rt, maps: []map[string]string{
		{"id": "r-1"},
		{"id": "r-2"},
	}})
	if got := m.resourceTable.Cursor(); got != 0 {
		t.Errorf("resource-table cursor = %d, want 0 (first resource)", got)
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
