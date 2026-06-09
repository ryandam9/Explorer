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

// TestSecurityGroupRowsDoNotWrap guards against the regression where the
// selected (highlighted) row in a table wider than the panel wrapped onto a
// second line, breaking column alignment. With clipping in place the rendered
// table must keep one screen line per row regardless of terminal width.
func TestSecurityGroupRowsDoNotWrap(t *testing.T) {
	items := buildSidebarItems()
	firstIdx := firstSelectableIdx(items)

	m := &Model{
		resourceMaps:     make(map[resourceType][]map[string]string),
		sidebarItems:     items,
		activeSidebarIdx: firstIdx,
		activeResource:   rtSecurityGroups,
		state:            stateResourceBrowser,
		focus:            focusResourceTable,
		region:           "ap-southeast-2",
		allVPCs:          []VPCInfo{{ID: "vpc-0475013d0d924", Name: ""}},
	}
	m.selectedVPC = &m.allVPCs[0]
	m.initResourceTable(m.activeResource)

	m.resourceMaps[rtSecurityGroups] = []map[string]string{
		sgToMap(SGInfo{ID: "sg-01b6690cf8c01ada1", Name: "default", Description: "default VPC security group"}),
		sgToMap(SGInfo{ID: "sg-070cc992c8b78edac", Name: "terraform-20221220005512345600000001", Description: "Managed by Terraform"}),
	}
	m.rebuildResourceTable()

	// Narrow terminal: the ~106-col SG table is wider than the right panel.
	m.width = 120
	m.height = 30
	m.updateTableSizes()

	// The whole browser must still fit and the table body must render one line
	// per row (header + 2 data rows = 3 lines), i.e. nothing wrapped.
	out := m.viewResourceBrowserState()
	if got := lipgloss.Height(out); got > m.height {
		t.Fatalf("browser overflows terminal: height=%d > %d", got, m.height)
	}

	panel := m.viewResourcePanel(m.height - 3)
	var contentLines int
	for _, ln := range strings.Split(panel, "\n") {
		if strings.Contains(ln, "sg-") || strings.Contains(ln, "SG ID") {
			contentLines++
		}
	}
	if contentLines != 3 {
		t.Errorf("expected 3 table lines (header + 2 rows), got %d — a row wrapped", contentLines)
	}
}
