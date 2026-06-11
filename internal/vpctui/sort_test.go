package vpctui

import "testing"

func TestResourceTableSorting(t *testing.T) {
	items := buildSidebarItems()
	firstIdx := firstSelectableIdx(items)
	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: firstIdx,
		activeResource:   rtSubnets,
		sortCol:          -1,
		sortAsc:          true,
		state:            stateResourceBrowser,
		focus:            focusResourceTable,
	}
	m.initResourceTable(rtSubnets)
	m.resourceMaps[rtSubnets] = []map[string]string{
		{"name": "beta", "cidr": "10.0.2.0/24"},
		{"name": "alpha", "cidr": "10.0.1.0/24"},
	}

	fields := m.colFields(rtSubnets)
	if len(fields) == 0 || fields[0].Key != "name" {
		t.Fatalf("expected name as the first subnet column, got %+v", fields)
	}

	// Sort by the first column ascending.
	m.sortCol = 0
	m.rebuildResourceTable()
	maps := m.resourceMaps[rtSubnets]
	if maps[0]["name"] != "alpha" {
		t.Fatalf("expected ascending sort by name, got %v", maps)
	}
	rows := m.resourceTable.Rows()
	if rows[0][1] != "alpha" {
		t.Fatalf("rows must mirror the sorted cache, got %v", rows[0])
	}
	cols := m.resourceTable.Columns()
	if got := cols[1].Title; got[len(got)-len(" ↑"):] != " ↑" {
		t.Errorf("active sort column should carry an arrow, got %q", got)
	}

	// Reverse.
	m.sortAsc = false
	m.rebuildResourceTable()
	if m.resourceMaps[rtSubnets][0]["name"] != "beta" {
		t.Fatalf("expected descending sort, got %v", m.resourceMaps[rtSubnets])
	}
}
