package vpctui

import "testing"

func TestJumpToLogsForSelected(t *testing.T) {
	newModel := func(rt resourceType, rows []map[string]string) *Model {
		items := buildSidebarItems()
		m := &Model{
			resourceMaps:     make(map[resourceType][]map[string]string),
			sidebarItems:     items,
			activeSidebarIdx: firstSelectableIdx(items),
			activeResource:   rt,
			sortCol:          -1,
			sortAsc:          true,
			state:            stateResourceBrowser,
			focus:            focusResourceTable,
		}
		m.initResourceTable(rt)
		m.resourceMaps[rt] = rows
		m.rebuildResourceTable()
		return m
	}

	// Lambda: the log group is derivable from the function name, so a jump
	// command is returned with no blocking reason.
	m := newModel(rtLambda, []map[string]string{{"name": "payments-fn"}})
	cmd, reason := m.jumpToLogsForSelected()
	if cmd == nil || reason != "" {
		t.Fatalf("lambda: want a jump command and no reason, got cmd=%v reason=%q", cmd != nil, reason)
	}

	// RDS: derivable from the DB instance id.
	m = newModel(rtRDS, []map[string]string{{"db_id": "orders-db", "engine": "postgres"}})
	cmd, reason = m.jumpToLogsForSelected()
	if cmd == nil || reason != "" {
		t.Fatalf("rds: want a jump command and no reason, got cmd=%v reason=%q", cmd != nil, reason)
	}

	// Subnet: not a logging resource, so no command and an explanatory reason.
	m = newModel(rtSubnets, []map[string]string{{"name": "alpha", "cidr": "10.0.0.0/24"}})
	cmd, reason = m.jumpToLogsForSelected()
	if cmd != nil || reason == "" {
		t.Fatalf("subnet: want no jump and a reason, got cmd=%v reason=%q", cmd != nil, reason)
	}
}
