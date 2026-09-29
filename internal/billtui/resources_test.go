package billtui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ryandam9/aws_explorer/internal/billing"
)

// resourcesModel opens the per-resource overlay over a service with n
// resources, in a terminal small enough that the list has to scroll.
func resourcesModel(t *testing.T, n int) Model {
	t.Helper()
	m := billModel(t, line("Amazon EC2", "BoxUsage", 4.15))
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	m = mm.(Model)
	rows := make([]billing.ResourceCost, 0, n)
	for i := range n {
		rows = append(rows, billing.ResourceCost{
			Resource: fmt.Sprintf("i-0%015x", i),
			Amount:   0.76 - float64(i)*0.02, Quantity: 5.93, Unit: "Hrs",
		})
	}
	m.overlay = overlayResources
	m.resService = "Amazon EC2"
	m.resRows = rows
	m.resStart = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	return m
}

// The overlay is one fixed size. It used to shrink as the list ran out —
// fewer rows rendered near the bottom, and the "… n more" line disappearing —
// so holding ↓ made the panel flicker and resize under the cursor.
func TestResourcesOverlayKeepsOneSize(t *testing.T) {
	m := resourcesModel(t, 29)
	want := lipgloss.Height(m.resourcesOverlay())
	wantW := lipgloss.Width(m.resourcesOverlay())

	for range 40 {
		m.scrollResources(1)
		if got := lipgloss.Height(m.resourcesOverlay()); got != want {
			t.Fatalf("height changed to %d at offset %d, want a fixed %d", got, m.resScroll, want)
		}
		if got := lipgloss.Width(m.resourcesOverlay()); got != wantW {
			t.Fatalf("width changed to %d at offset %d, want a fixed %d", got, m.resScroll, wantW)
		}
	}

	// A list that does not fill one page renders the same box, not a short one.
	short := resourcesModel(t, 3)
	if got := lipgloss.Height(short.resourcesOverlay()); got != want {
		t.Errorf("a 3-resource list renders %d lines, want the same %d", got, want)
	}

	// And the whole thing fits the terminal it is drawn in.
	if want > 34 {
		t.Errorf("overlay is %d lines tall in a 34-line terminal", want)
	}
}

// Scrolling stops at the last full page: past it the panel would show a
// handful of rows over empty space.
func TestResourcesScrollStopsAtAFullPage(t *testing.T) {
	m := resourcesModel(t, 29)
	rows := m.resVisibleRows()
	if rows < 5 || rows >= 29 {
		t.Fatalf("test needs a list longer than one page: %d rows visible of 29", rows)
	}

	m.scrollResources(1000)
	if m.resScroll != 29-rows {
		t.Errorf("scrolled to %d, want the last full page at %d", m.resScroll, 29-rows)
	}
	if got := m.resourcesOverlay(); !strings.Contains(got, fmt.Sprintf("%d–29 of 29", 29-rows+1)) {
		t.Errorf("footer does not report the position at the end")
	}

	m.scrollResources(-1000)
	if m.resScroll != 0 {
		t.Errorf("scrolled back to %d, want 0", m.resScroll)
	}

	// A list that fits has nowhere to scroll.
	short := resourcesModel(t, 3)
	short.scrollResources(10)
	if short.resScroll != 0 {
		t.Errorf("a 3-row list scrolled to %d, want 0", short.resScroll)
	}
}

// The list has a scrollbar, and its gutter is reserved whether or not there is
// anything to scroll — so the rows do not shift when the list grows.
func TestResourcesOverlayHasAScrollbar(t *testing.T) {
	long := resourcesModel(t, 29).resourcesOverlay()
	if !strings.Contains(long, "┃") {
		t.Error("no scrollbar thumb on a list longer than the panel")
	}
	short := resourcesModel(t, 3).resourcesOverlay()
	if strings.Contains(short, "┃") {
		t.Error("a list that fits should have a blank gutter, not a thumb")
	}
	if lipgloss.Width(long) != lipgloss.Width(short) {
		t.Error("the gutter is not reserved: the panel changes width with the list")
	}
}

// The window total and the bill total are both named. They cover different
// periods — 14 rolling days against the billing period — so showing only the
// smaller one reads as a discrepancy. (Reported: summary said $0.12, the
// resource list said $0.05.)
func TestResourcesOverlayNamesBothTotals(t *testing.T) {
	m := billModel(t,
		line("Amazon ECR", "Storage", 0.07),
		line("Amazon ECR", "DataTransfer", 0.05),
	)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 34})
	m = mm.(Model)
	m.overlay = overlayResources
	m.resService = "Amazon ECR"
	m.resStart = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	m.resRows = []billing.ResourceCost{{Resource: "NoResourceId", Amount: 0.05, Quantity: 0.53, Unit: "GB-Month"}}

	// Asserted phrase by phrase: the note is wrapped to the panel, so a line
	// break can fall anywhere in it.
	got := m.resourcesOverlay()
	for _, want := range []string{
		"$0.05", "across 1 resource", "last 14 days", // what this window holds
		"$0.12", "on the bill", // and what the service costs over the period
	} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay is missing %q, so the two totals look inconsistent:\n%s", want, got)
		}
	}
}
