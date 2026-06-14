package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newTestModel builds a model, sizes it, and feeds it a chunk of fake
// resources, mimicking the startup sequence without touching AWS.
func newTestModel(t *testing.T, width, height int) tuiModel {
	t.Helper()
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)

	resources := []model.Resource{
		{Service: "s3", Type: "bucket", Region: "us-east-1", ID: "bkt-1", Name: "logs", State: "active"},
		{Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-abc123", Name: "web-1", State: "running"},
		{Service: "ec2", Type: "instance", Region: "eu-west-1", ID: "i-def456", Name: "web-2", State: "running"},
	}

	m = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: resources}})
	return m
}

func update(m tuiModel, msg tea.Msg) tuiModel {
	next, _ := m.Update(msg)
	return next.(tuiModel)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestViewShowsResourcesAndContextHints(t *testing.T) {
	m := newTestModel(t, 140, 40)
	plain := ansi.Strip(m.View())

	for _, want := range []string{"i-abc123", "web-1", "Services", "ec2", "s3"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view missing %q", want)
		}
	}
	// Table focus: the status bar advertises table shortcuts.
	for _, want := range []string{"navigate", "detail", "filter", "help"} {
		if !strings.Contains(plain, want) {
			t.Errorf("status bar missing table-context hint %q", want)
		}
	}
}

func TestDetailOverlayChangesHints(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, key("enter")) // open detail for the selected row
	if !m.showDetail {
		t.Fatal("enter on table should open the detail panel")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "close") {
		t.Errorf("detail-focus status bar should offer Esc close, got %q", lastLine(plain))
	}
	m = update(m, key("esc"))
	if m.showDetail {
		t.Error("esc should close the detail panel")
	}
}

func TestQuickTextFilter(t *testing.T) {
	m := newTestModel(t, 140, 40)
	m = update(m, key("/"))
	if !m.filtering {
		t.Fatal("/ should enter filter mode")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "Enter keep filter") {
		t.Errorf("filter-mode hints not shown: %q", lastLine(plain))
	}

	m = update(m, key("web-2"))
	rows := m.allRows[m.currentService()]
	if len(rows) != 1 {
		t.Fatalf("filter 'web-2' should leave 1 row, got %d", len(rows))
	}
	if res, ok := m.selectedResource(); !ok || res.ID != "i-def456" {
		t.Errorf("selected resource = %+v, want i-def456", res)
	}

	// Esc clears the filter entirely.
	m = update(m, key("esc"))
	if m.filterText != "" || len(m.allRows[m.currentService()]) != 3 {
		t.Errorf("esc should clear the filter, got text=%q rows=%d",
			m.filterText, len(m.allRows[m.currentService()]))
	}
}

func TestNarrowTerminalScrollsColumns(t *testing.T) {
	m := newTestModel(t, 70, 30)
	l, r := m.table.ColScrollInfo()
	if l+r == 0 {
		t.Fatal("narrow terminal should hide columns and enable scrolling")
	}
	plain := ansi.Strip(m.View())
	if !strings.Contains(plain, "more cols") {
		t.Error("scroll indicator missing from narrow view")
	}
	if !hasHint(m.statusHints(), "</>") {
		t.Error("status hints should advertise </> column scrolling when columns are hidden")
	}

	m = update(m, key(">"))
	if l2, _ := m.table.ColScrollInfo(); l2 != 1 {
		t.Errorf("> should scroll one column right, hiddenLeft=%d", l2)
	}
}

func TestResetHintOnlyWithActiveFilter(t *testing.T) {
	m := newTestModel(t, 140, 40)
	if hasHint(m.statusHints(), "r") {
		t.Error("reset hint shown with no active filter")
	}
	m.filterText = "web"
	if !hasHint(m.statusHints(), "r") {
		t.Error("reset hint missing while a filter is active")
	}
}

func TestErrorsOverlay(t *testing.T) {
	m := newTestModel(t, 140, 40)

	// With no errors, 'e' is a no-op and no errors hint is offered.
	if hasHint(m.statusHints(), "e") {
		t.Error("errors hint shown with no errors collected")
	}
	m = update(m, key("e"))
	if m.showErrors {
		t.Fatal("'e' should do nothing when there are no errors")
	}

	// Feed an access-denied error, then open the overlay.
	m = update(m, chunkMsg{chunk: model.ResultChunk{Errors: []model.ExploreError{{
		Service: "rds", Region: "us-east-1", Code: "AccessDenied",
		Message: "Insufficient privileges — required IAM permission: rds:DescribeDBInstances",
	}}}})
	if !hasHint(m.statusHints(), "e") {
		t.Error("errors hint missing once an error was collected")
	}

	m = update(m, key("e"))
	if !m.showErrors {
		t.Fatal("'e' should open the errors overlay when errors exist")
	}
	plain := ansi.Strip(m.View())
	for _, want := range []string{"INSUFFICIENT PRIVILEGES", "RDS", "rds:DescribeDBInstances"} {
		if !strings.Contains(plain, want) {
			t.Errorf("errors overlay missing %q", want)
		}
	}

	// Esc closes it again.
	m = update(m, key("esc"))
	if m.showErrors {
		t.Error("esc should close the errors overlay")
	}
}

func hasHint(hints []ui.KeyHint, key string) bool {
	for _, h := range hints {
		if h.Key == key {
			return true
		}
	}
	return false
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// ── Incremental merge behaviour ──────────────────────────────────────────────

func TestChunksMergeSortedAndDedupedByARN(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// First chunk: an s3 entry plus a sparse sweep-style entry (ARN, no state).
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "s3", Type: "bucket", Name: "logs", ID: "b1"},
		{Service: "ec2", Type: "instance", Name: "web", ID: "arn-only", ARN: "arn:aws:ec2:i-1"},
	}}})
	// Second chunk arrives out of order: a richer typed entry for the same
	// ARN, and a service that sorts before the existing ones.
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", ID: "i-1", ARN: "arn:aws:ec2:i-1", State: "running"},
		{Service: "cloudwatch", Type: "alarm", Name: "alarm-1", ID: "a1"},
	}}})

	if len(m.sorted) != 3 {
		t.Fatalf("expected 3 resources after dedupe, got %d: %+v", len(m.sorted), m.sorted)
	}
	wantOrder := []string{"cloudwatch", "ec2", "s3"}
	for i, svc := range wantOrder {
		if m.sorted[i].Service != svc {
			t.Errorf("sorted[%d].Service = %q, want %q", i, m.sorted[i].Service, svc)
		}
	}
	// The richer typed entry must have replaced the sparse one.
	if m.sorted[1].ID != "i-1" || m.sorted[1].State != "running" {
		t.Errorf("expected the richer entry to win the ARN dedupe, got %+v", m.sorted[1])
	}
	// The parallel search-text slice stays in sync and pre-lowered.
	if len(m.searchText) != len(m.sorted) {
		t.Fatalf("searchText out of sync: %d vs %d", len(m.searchText), len(m.sorted))
	}
	if !strings.Contains(m.searchText[1], "running") {
		t.Errorf("searchText[1] should contain the replacement's state, got %q", m.searchText[1])
	}
	if idx, ok := m.byARN["arn:aws:ec2:i-1"]; !ok || idx != 1 {
		t.Errorf("byARN index = %d (ok=%v), want 1", idx, ok)
	}
}

func TestRowsAreBuiltLazilyPerService(t *testing.T) {
	m := newTestModel(t, 120, 40)

	// Only the displayed group ("All") is materialized after a chunk.
	if _, ok := m.allRows["All"]; !ok {
		t.Fatal("the displayed group should be cached after a chunk")
	}
	if _, ok := m.allRows["ec2"]; ok {
		t.Fatal("undisplayed service groups should not be built eagerly")
	}

	rows := m.rowsFor("ec2")
	if len(rows) != 2 {
		t.Fatalf("expected 2 ec2 rows, got %d", len(rows))
	}
	if _, ok := m.allRows["ec2"]; !ok {
		t.Fatal("rowsFor should cache the group it built")
	}
}

// ── UI feature tests ─────────────────────────────────────────────────────────

func TestColumnSortCycleAndReverse(t *testing.T) {
	m := newTestModel(t, 120, 40)

	// Natural order ("All"): grouped by service → ec2, ec2, s3.
	rows := m.rowsFor("All")
	if rows[0][1] != "ec2" || rows[2][1] != "s3" {
		t.Fatalf("unexpected natural order: %v", rows)
	}

	// 's' once sorts by Service; cycle to column 5 (Name).
	for range 5 {
		m = update(m, key("s"))
	}
	if m.sortCol != 5 {
		t.Fatalf("sortCol = %d after 5 presses, want 5 (Name)", m.sortCol)
	}
	rows = m.rowsFor("All")
	if rows[0][5] != "logs" || rows[1][5] != "web-1" || rows[2][5] != "web-2" {
		t.Fatalf("expected name-ascending order, got %v", rows)
	}

	// 'R' reverses.
	m = update(m, key("R"))
	rows = m.rowsFor("All")
	if rows[0][5] != "web-2" || rows[2][5] != "logs" {
		t.Fatalf("expected name-descending order, got %v", rows)
	}

	// Sort indicator shows on the active column title.
	cols := m.columns()
	if !strings.Contains(cols[5].Title, "↓") {
		t.Errorf("active sort column should carry a direction arrow, got %q", cols[5].Title)
	}
}

func TestSidebarShowsErrorBadges(t *testing.T) {
	m := newTestModel(t, 120, 40)
	m = update(m, chunkMsg{chunk: model.ResultChunk{Errors: []model.ExploreError{{
		Service: "ec2", Region: "us-east-1", Code: "AccessDenied", Message: "denied",
	}}}})

	sidebar := m.renderSidebar()
	plain := ansi.Strip(sidebar)
	if !strings.Contains(plain, "ec2 ⚠1") {
		t.Errorf("sidebar should badge the failing service, got:\n%s", plain)
	}
}

func TestScanProgressCountsTasks(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	// Simulate a planned scan of two tasks.
	m.tasksTotal = 2
	m.tasksPending = map[string]bool{"ec2@us-east-1": true, "s3@global": true}

	m = update(m, chunkMsg{chunk: model.ResultChunk{
		Progress: &model.TaskProgress{Service: "ec2", Region: "us-east-1"},
	}})
	if m.tasksDone != 1 {
		t.Fatalf("tasksDone = %d, want 1", m.tasksDone)
	}
	if got := m.scanStatus(); !strings.Contains(got, "1/2") || !strings.Contains(got, "s3@global") {
		t.Errorf("scanStatus = %q, want progress count and pending task name", got)
	}
	// A duplicate progress marker must not double-count.
	m = update(m, chunkMsg{chunk: model.ResultChunk{
		Progress: &model.TaskProgress{Service: "ec2", Region: "us-east-1"},
	}})
	if m.tasksDone != 1 {
		t.Fatalf("duplicate progress marker double-counted: %d", m.tasksDone)
	}
}

func TestFilterShowsMatchCount(t *testing.T) {
	m := newTestModel(t, 120, 40)
	m = update(m, key("/"))
	for _, r := range "web" {
		m = update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	panel := ansi.Strip(m.renderTablePanel())
	if !strings.Contains(panel, "2/3 match") {
		t.Errorf("filter line should show the match count, got:\n%s", panel)
	}
}

func TestStaleGenerationChunksDropped(t *testing.T) {
	m := newTestModel(t, 120, 40)
	before := len(m.sorted)
	m.scanGen = 1 // as after a profile/region switch
	m = update(m, chunkMsg{gen: 0, chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "rds", Type: "db", ID: "stale"},
	}}})
	if len(m.sorted) != before {
		t.Fatalf("stale-generation chunk was merged: %d -> %d", before, len(m.sorted))
	}
}

func TestInventoryCSV(t *testing.T) {
	header, rows := inventoryCSV([]model.Resource{{
		Service: "ec2", Type: "instance", Region: "us-east-1", ID: "i-1",
		Name: "web", State: "running", ARN: "arn:aws:ec2:i-1",
		Tags: map[string]string{"env": "prod", "app": "api"},
	}})
	if header[0] != "Service" || header[len(header)-1] != "Tags" {
		t.Fatalf("unexpected header: %v", header)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	row := rows[0]
	if row[0] != "ec2" || row[5] != "i-1" || row[8] != "arn:aws:ec2:i-1" {
		t.Errorf("unexpected row: %v", row)
	}
	if row[10] != "app=api; env=prod" {
		t.Errorf("tags should be sorted key=value pairs, got %q", row[10])
	}
}

func TestRawJSONDetailToggle(t *testing.T) {
	m := newTestModel(t, 160, 40)
	m = update(m, key("enter")) // open detail for the selected resource
	if !m.showDetail {
		t.Fatal("detail should be open")
	}
	m = update(m, key("J"))
	if !m.detailRaw {
		t.Fatal("J should enable the raw JSON view")
	}
	body := m.renderDetail(*m.detail, 60)
	if !strings.Contains(body, `"service"`) {
		t.Errorf("raw detail should render JSON, got:\n%s", body)
	}
	m = update(m, key("J"))
	if m.detailRaw {
		t.Fatal("J should toggle the raw JSON view off")
	}
}

func TestMergeUpdatesStateOnRescan(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", ID: "i-1", ARN: "arn:aws:ec2:i-1", State: "running"},
	}}})

	// Watch-style re-scan: identical richness, different state.
	m.snapshotStates()
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", ID: "i-1", ARN: "arn:aws:ec2:i-1", State: "stopped"},
	}}})

	if len(m.sorted) != 1 {
		t.Fatalf("expected 1 resource after re-scan, got %d", len(m.sorted))
	}
	if m.sorted[0].State != "stopped" {
		t.Errorf("state not updated on re-scan: got %q, want %q", m.sorted[0].State, "stopped")
	}
	if _, ok := m.changedRows["arn:aws:ec2:i-1"]; !ok {
		t.Errorf("state transition not flagged in changedRows")
	}
}

func TestMergeDedupesARNLessResources(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	r := model.Resource{Service: "s3", Type: "bucket", Region: "us-east-1", ID: "b1", Name: "logs", State: "active"}
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{r}}})
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{r}}})

	if len(m.sorted) != 1 {
		t.Fatalf("ARN-less resource duplicated on re-scan: got %d rows, want 1", len(m.sorted))
	}
}

// The support panes (t/l/g/x) append long lines — error messages, log lines,
// resource IDs — to the detail body. The detail viewport clips rather than
// wraps, so these must be wrapped to the panel width or they get truncated at
// the right edge (issue #148).
func TestDetailSupportPanesWrapToWidth(t *testing.T) {
	m := newTestModel(t, 160, 40)
	m = update(m, key("enter")) // open detail for the selected resource
	if !m.showDetail || m.detail == nil {
		t.Fatal("detail should be open")
	}

	const longToken = "sg-0123456789abcdef0123456789abcdef0123456789"
	m.showXref = true
	m.xrefResources = []model.Resource{
		{Service: "ec2", Type: "security-group", ID: longToken, Name: "ref"},
	}
	m.showLogs = true
	m.logsLines = []string{"2026-06-14T01:02:03Z ERROR handler failed with a very long descriptive message that overflows the panel"}
	m.showTimeline = true
	m.timelineErr = errors.New("AccessDenied: user is not authorized to perform cloudtrail:LookupEvents on this resource")

	const width = 32
	body := m.renderDetail(*m.detail, width)
	for _, line := range strings.Split(ansi.Strip(body), "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("detail line exceeds panel width %d (got %d): %q", width, w, line)
		}
	}

	// Wrapping must not drop content: a long unbreakable token is hard-broken
	// across lines (with line padding between chunks) but every character
	// survives. Collapse all whitespace before checking it is intact.
	collapsed := strings.Join(strings.Fields(ansi.Strip(body)), "")
	if !strings.Contains(collapsed, longToken) {
		t.Errorf("xref resource ID lost during wrapping:\n%s", ansi.Strip(body))
	}
}

// The detail panel grows into spare horizontal space on wide terminals (so
// messages aren't needlessly truncated) but stays at the minimum when there is
// none, and never exceeds its cap.
func TestDetailWidthIsResponsive(t *testing.T) {
	narrow := newTestModel(t, sidebarInner+4+minTableInner+6+detailInner+4, 40)
	if got := narrow.detailWidth(); got != detailInner {
		t.Errorf("at the minimum inline width the detail panel should be %d, got %d", detailInner, got)
	}

	wide := newTestModel(t, 400, 40)
	if got := wide.detailWidth(); got != detailInnerMax {
		t.Errorf("on a very wide terminal the detail panel should cap at %d, got %d", detailInnerMax, got)
	}

	mid := newTestModel(t, 130, 40)
	if got := mid.detailWidth(); got <= detailInner || got > detailInnerMax {
		t.Errorf("on a mid-width terminal the detail panel should grow past the minimum but stay under the cap, got %d", got)
	}
}

func TestFinishWatchSweepPrunesVanishedResources(t *testing.T) {
	m := NewModel(context.Background(), nil, "", &config.Config{}).(tuiModel)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", ID: "i-1", ARN: "arn:aws:ec2:i-1", State: "running"},
		{Service: "ec2", Type: "instance", Name: "old", ID: "i-2", ARN: "arn:aws:ec2:i-2", State: "running"},
	}}})

	// Watch refresh in which only i-1 reappears.
	m.watchSeen = make(map[string]bool)
	m = update(m, chunkMsg{chunk: model.ResultChunk{Resources: []model.Resource{
		{Service: "ec2", Type: "instance", Name: "web", ID: "i-1", ARN: "arn:aws:ec2:i-1", State: "running"},
	}}})
	m.finishWatchSweep()

	if len(m.sorted) != 1 {
		t.Fatalf("expected vanished resource pruned, got %d rows: %+v", len(m.sorted), m.sorted)
	}
	if m.sorted[0].ID != "i-1" {
		t.Errorf("wrong resource survived the sweep: %+v", m.sorted[0])
	}
	if m.watchSeen != nil {
		t.Errorf("watchSeen should be reset after the sweep")
	}
}
