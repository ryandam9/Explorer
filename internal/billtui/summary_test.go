package billtui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/ryandam9/aws_explorer/internal/billing"
)

func TestSummarizeByService(t *testing.T) {
	got := summarizeByService([]billing.Line{
		line("Amazon S3", "TimedStorage", 1.00),
		line("Amazon EC2", "BoxUsage", 5.00),
		line("Amazon EC2", "DataTransfer-In", 0),
		line("Amazon EC2", "EBS:VolumeUsage", 3.00),
		line("AWS Lambda", "Requests", 0),
	})

	if len(got) != 3 {
		t.Fatalf("got %d services, want 3", len(got))
	}
	// Cost descending: EC2 (8.00), S3 (1.00), Lambda (0).
	if got[0].Service != "Amazon EC2" || got[1].Service != "Amazon S3" || got[2].Service != "AWS Lambda" {
		t.Errorf("order = %q, %q, %q; want EC2, S3, Lambda", got[0].Service, got[1].Service, got[2].Service)
	}
	if got[0].Amount != 8.00 {
		t.Errorf("EC2 total = %v, want 8.00", got[0].Amount)
	}
	if got[0].Lines != 3 || got[0].Zero != 1 {
		t.Errorf("EC2 = %d line(s) / %d free, want 3 / 1", got[0].Lines, got[0].Zero)
	}
	// A service that billed nothing is still a row: "AWS Lambda ran and cost
	// nothing" is a different answer from "AWS Lambda is not on this bill".
	if got[2].Amount != 0 || got[2].Lines != 1 {
		t.Errorf("free service = %+v, want one line at zero", got[2])
	}
}

// Equal totals sort by name, so a refresh doesn't shuffle rows under the
// cursor — the long free tail is where ties are the rule, not the exception.
func TestSummarizeByServiceStableOnTies(t *testing.T) {
	got := summarizeByService([]billing.Line{
		line("zebra", "u", 0),
		line("alpha", "u", 0),
		line("mango", "u", 0),
	})
	var names []string
	for _, s := range got {
		names = append(names, s.Service)
	}
	if strings.Join(names, ",") != "alpha,mango,zebra" {
		t.Errorf("tie order = %v, want alpha,mango,zebra", names)
	}
}

func TestSummarizeByServiceEmpty(t *testing.T) {
	if got := summarizeByService(nil); len(got) != 0 {
		t.Errorf("got %d rows for no lines, want 0", len(got))
	}
}

func TestShareOfEmptyBillIsNotNaN(t *testing.T) {
	if got := shareOf(0, 0); got != 0 {
		t.Errorf("shareOf(0, 0) = %v, want 0 — an all-free bill must render, not print NaN", got)
	}
	if got := shareOf(25, 100); got != 25 {
		t.Errorf("shareOf(25, 100) = %v, want 25", got)
	}
}

func TestDropAndCountZeroCost(t *testing.T) {
	lines := []billing.Line{
		line("EC2", "BoxUsage", 5),
		line("EC2", "DataTransfer-In", 0),
		line("S3", "Requests", 0),
	}
	if got := countZeroCost(lines); got != 2 {
		t.Errorf("countZeroCost = %d, want 2", got)
	}
	kept := dropZeroCost(lines)
	if len(kept) != 1 || kept[0].UsageType != "BoxUsage" {
		t.Errorf("dropZeroCost kept %+v, want only the charged line", kept)
	}
	// The input is left alone: the detail view still has every line to go back to.
	if len(lines) != 3 {
		t.Errorf("dropZeroCost mutated its input: %d lines left", len(lines))
	}
}

// T swaps the usage-type table for the rollup, and the rollup's rows carry the
// per-service totals rather than the first line of each service.
func TestSummaryToggleBuildsServiceRows(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon EC2", "EBS", 3),
		line("Amazon S3", "TimedStorage", 2),
	)
	mm, _ := m.Update(key("T"))
	m = mm.(Model)

	if !m.summary {
		t.Fatal("T did not enter the summary")
	}
	rows := m.tbl.Rows()
	if len(rows) != 2 {
		t.Fatalf("got %d summary rows, want 2 (one per service)", len(rows))
	}
	if rows[0][1] != "Amazon EC2" || rows[0][2] != "$8.00" {
		t.Errorf("first row = %v, want Amazon EC2 at $8.00", rows[0])
	}
	if rows[0][3] != "80.0%" {
		t.Errorf("share = %q, want 80.0%% of the $10.00 shown", rows[0][3])
	}
	if len(rows[0]) != len(summaryColumns) {
		t.Errorf("row has %d cells, want %d — the summary answers what you pay for, not how many usage types it took",
			len(rows[0]), len(summaryColumns))
	}

	// And back: the detail view returns with its own columns.
	mm, _ = m.Update(key("T"))
	m = mm.(Model)
	if m.summary || len(m.tbl.Rows()) != 3 {
		t.Errorf("T did not return to the 3-line detail view (summary=%v, rows=%d)", m.summary, len(m.tbl.Rows()))
	}
}

// The footer reports the total of the rows on screen. With a filter applied
// that is not the whole bill, so it names both rather than letting one stand
// for the other.
func TestSummaryFooterTotalsTheRowsShown(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon S3", "TimedStorage", 2),
	)
	m.bill.Total = 7
	mm, _ := m.Update(key("T"))
	m = mm.(Model)

	if got := m.footerView(); !strings.Contains(got, "$7.00") || strings.Contains(got, "billed") {
		t.Errorf("unfiltered footer = %q, want the plain $7.00 total", got)
	}

	m.filter.SetValue("EC2")
	m.rebuild()
	got := m.footerView()
	if !strings.Contains(got, "$5.00") {
		t.Errorf("filtered footer = %q, want the $5.00 shown", got)
	}
	if !strings.Contains(got, "of $7.00 billed") {
		t.Errorf("filtered footer = %q, want it to name the whole-bill total too", got)
	}
}

// z hides the free lines, and the footer says how many — a shorter table with
// no explanation reads as a shorter bill.
func TestHideZeroCostIsCounted(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon EC2", "DataTransfer-In", 0),
		line("AWS Lambda", "Requests", 0),
	)
	// Hidden by default — the screen opens on what is being charged.
	if len(m.visible) != 1 {
		t.Fatalf("opened with %d lines, want only the charged one", len(m.visible))
	}
	if got := m.footerView(); !strings.Contains(got, "hiding 2 line(s)") {
		t.Errorf("footer = %q, want it to report the 2 hidden lines", got)
	}

	// z brings them back, so hiding is a view, not a loss.
	mm, _ := m.Update(key("z"))
	m = mm.(Model)
	if len(m.visible) != 3 {
		t.Fatalf("z left %d lines, want all 3", len(m.visible))
	}
	if got := m.footerView(); !strings.Contains(got, "2 line(s) carry no cost") {
		t.Errorf("footer = %q, want the free-line count", got)
	}
}

// The summary lists what costs money. A service whose total is zero — every
// line free, or a charge and a credit that cancel — is not a row until asked
// for, and the footer says how many were left out.
func TestSummaryHidesZeroCostServices(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("AWS Glue", "Crawler", 0),
		line("AWS Lambda", "Requests", 0),
		// Nets to zero without a single zero line: a charge and its credit.
		line("Amazon S3", "Storage", 2),
		line("Amazon S3", "Credit", -2),
	)
	mm, _ := m.Update(key("T"))
	m = mm.(Model)

	var names []string
	for _, r := range m.tbl.Rows() {
		names = append(names, r[1])
	}
	if strings.Join(names, ",") != "Amazon EC2" {
		t.Errorf("summary shows %v, want only the service that cost something", names)
	}
	if m.zeroSvc != 3 {
		t.Errorf("zeroSvc = %d, want 3 (Glue, Lambda and the netted-out S3)", m.zeroSvc)
	}
	if got := m.footerView(); !strings.Contains(got, "hiding") || !strings.Contains(got, "3 services that cost nothing") {
		t.Errorf("footer = %q, want it to name the 3 hidden services", got)
	}

	// z shows them again.
	mm, _ = m.Update(key("z"))
	m = mm.(Model)
	if got := len(m.tbl.Rows()); got != 4 {
		t.Errorf("z showed %d services, want all 4", got)
	}
}

// In the summary the cursor indexes services, so there is no single bill line
// under it — the detail overlay must not describe whichever line shares the
// index.
func TestSummaryHasNoSelectedLine(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon S3", "TimedStorage", 2),
	)
	mm, _ := m.Update(key("T"))
	m = mm.(Model)

	if m.selected() != nil {
		t.Error("summary mode returned a bill line for the cursor")
	}
	if got := m.selectedService(); got != "Amazon EC2" {
		t.Errorf("selectedService = %q, want Amazon EC2", got)
	}
}

// Enter on a summary row drills into that service's usage types, using the
// filter the detailed view already has.
func TestSummaryEnterDrillsIntoService(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon EC2", "EBS", 3),
		line("Amazon S3", "TimedStorage", 2),
	)
	mm, _ := m.Update(key("T"))
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mm.(Model)

	if m.summary {
		t.Error("enter should leave the summary for the lines behind the row")
	}
	if m.filter.Value() != "Amazon EC2" {
		t.Errorf("filter = %q, want the drilled-into service", m.filter.Value())
	}
	if len(m.visible) != 2 {
		t.Errorf("drill-down shows %d lines, want EC2's 2", len(m.visible))
	}
	if m.overlay != overlayNone {
		t.Error("enter opened the line detail overlay from the summary")
	}
}

// x drills into resources from either view, so the summary is not a dead end.
func TestSummaryResourceDrillDownUsesTheService(t *testing.T) {
	m := billModel(t, line("Amazon EC2", "BoxUsage", 5))
	mm, _ := m.Update(key("T"))
	m = mm.(Model)
	mm, _ = m.Update(key("x"))
	m = mm.(Model)

	if m.overlay != overlayResources || m.resService != "Amazon EC2" {
		t.Errorf("x from the summary: overlay=%v service=%q, want the resources overlay for Amazon EC2",
			m.overlay, m.resService)
	}
}

func TestSortTotals(t *testing.T) {
	m := &Model{summary: true, totals: []ServiceTotal{
		{Service: "B", Amount: 9, Lines: 1},
		{Service: "A", Amount: 1, Lines: 7},
	}}
	m.sortCol, m.sortAsc = 1, true // SERVICE, A→Z
	m.sortTotals()
	if m.totals[0].Service != "A" {
		t.Errorf("service-asc first = %q, want A", m.totals[0].Service)
	}

	m.sortCol, m.sortAsc = 2, false // COST, biggest first
	m.sortTotals()
	if m.totals[0].Amount != 9 {
		t.Errorf("cost-desc first = %v, want 9", m.totals[0].Amount)
	}

	m.sortCol, m.sortAsc = 3, false // SHARE ranks the same as COST
	m.sortTotals()
	if m.totals[0].Amount != 9 {
		t.Errorf("share-desc first = %v, want 9", m.totals[0].Amount)
	}

	m.sortCol = -1 // natural ranking: untouched
	before := append([]ServiceTotal(nil), m.totals...)
	m.sortTotals()
	for i := range before {
		if m.totals[i] != before[i] {
			t.Fatalf("col -1 reordered the rollup at %d", i)
		}
	}
}

// The footer is an extra line between the table and the status bar, so the
// table has to be re-measured when it appears — otherwise the frame grows by
// one row and ClipToSize trims the status bar off the bottom.
func TestSummaryFooterDoesNotClipStatusBar(t *testing.T) {
	m := billModel(t,
		line("Amazon EC2", "BoxUsage", 5),
		line("Amazon EC2", "DataTransfer-In", 0),
		line("Amazon S3", "TimedStorage", 2),
	)
	m.bill.Total = 7
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 16})
	m = mm.(Model)
	mm, _ = m.Update(key("T"))
	m = mm.(Model)

	lines := strings.Split(m.View().Content, "\n")
	if len(lines) > 16 {
		t.Errorf("frame is %d lines tall, want at most the terminal's 16", len(lines))
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "sort") {
		t.Errorf("status bar should be the last line; got %q", last)
	}
	if !strings.Contains(m.View().Content, "total $7.00") {
		t.Error("the summary total is not on screen")
	}
}

// Switching views resets the sort: the column it named belongs to the other
// table, and index 5 there is a different field.
func TestSummaryToggleResetsSort(t *testing.T) {
	m := billModel(t, line("Amazon EC2", "BoxUsage", 5))
	m.sortCol, m.sortAsc = 6, true // CHANGE, a detail-only column
	mm, _ := m.Update(key("T"))
	m = mm.(Model)

	if m.sortCol != -1 {
		t.Errorf("sortCol = %d after switching views, want -1", m.sortCol)
	}
}
