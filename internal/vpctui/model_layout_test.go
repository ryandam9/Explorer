package vpctui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResourceBrowserFitsHeight(t *testing.T) {
	items := buildSidebarItems()
	firstIdx := firstSelectableIdx(items)

	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: firstIdx,
		activeResource:   items[firstIdx].rt,
		state:            stateResourceBrowser,
		focus:            focusResourceTable,
		region:           "ap-southeast-2",
		allVPCs:          []VPCInfo{{ID: "vpc-0475013d0d924", Name: ""}},
	}
	m.selectedVPC = &m.allVPCs[0]
	m.initResourceTable(m.activeResource)

	for _, h := range []int{30, 24, 40} {
		m.width = 160
		m.height = h
		m.updateTableSizes()
		out := m.viewResourceBrowserState()
		gotH := lipgloss.Height(out)
		if gotH > h {
			t.Errorf("height=%d: rendered %d rows, overflows terminal", h, gotH)
		}
		first := strings.SplitN(out, "\n", 2)[0]
		if !strings.ContainsAny(first, "╭─╮") {
			t.Errorf("height=%d: first line is not a top border (got %q)", h, first)
		}
		if !strings.Contains(out, "VPCs") || !strings.Contains(out, "Resources") || !strings.Contains(out, "Subnets") {
			t.Errorf("height=%d: a panel title is missing from output", h)
		}
	}
}
