package emrtui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/table"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// newClusterTestModel builds an EMR model wired with the shared table and a
// small inventory, sized to w×h, for the cluster-list tests.
func newClusterTestModel(w, h int) *m {
	mm := &m{
		regions: []string{"us-east-1"},
		filter:  textinput.New(),
		sortCol: -1,
		tbl: table.New(
			table.WithColumns(clusterColumns(false)),
			table.WithFocused(true),
			table.WithStyles(ui.TableStyles()),
			table.WithFrozenColumns(1),
		),
		stepsTbl: newSubTable(stepColumns()),
		yarnTbl:  newSubTable(yarnColumns()),
		hbaseTbl: newSubTable(hbaseColumns()),
		oozieTbl: newSubTable(oozieWFColumns()),
		inv: Inventory{Clusters: []Cluster{
			{Name: "data-platform-production-analytics-cluster-2026", ID: "j-2AB3CD4EF5", State: "WAITING", ReleaseLabel: "emr-7.1.0", Applications: "Spark, HBase, Hive, Hadoop, Tez, Livy", InstanceHours: 128},
			{Name: "etl", ID: "j-9ZY8XW7VU6", State: "TERMINATED_WITH_ERRORS", ReleaseLabel: "emr-6.15.0", Applications: "Spark", InstanceHours: 12, StateReason: "boom"},
			{Name: "mid", ID: "j-5MID000000", State: "RUNNING", ReleaseLabel: "emr-7.0.0", Applications: "Spark", InstanceHours: 64},
		}},
	}
	mm.width, mm.height = w, h
	mm.rebuild()
	return mm
}

// No rendered line may exceed the terminal width (the bug that motivated the
// migration), and the status bar with its shortcuts must always be present.
func TestClusterTableNeverWraps(t *testing.T) {
	for _, w := range []int{200, 120, 100, 80, 60} {
		mm := newClusterTestModel(w, 24)
		out := mm.View()
		for i, line := range strings.Split(out, "\n") {
			if lw := ansi.StringWidth(line); lw > w {
				t.Errorf("width %d: line %d overflows (%d > %d): %q", w, i, lw, w, line)
			}
		}
		if !strings.Contains(out, "rows") || !strings.Contains(out, "quit") {
			t.Errorf("width %d: status hints missing", w)
		}
	}
}

func TestClusterTableNavSelectsCluster(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	if cl, ok := mm.selectedCluster(); !ok || cl.Name != mm.view[0].Name {
		t.Fatalf("initial selection = %+v, ok=%v", cl, ok)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if got := mm.tbl.Cursor(); got != 1 {
		t.Fatalf("cursor after j = %d, want 1", got)
	}
	if cl, _ := mm.selectedCluster(); cl.Name != mm.view[1].Name {
		t.Errorf("selection after j = %q, want %q", cl.Name, mm.view[1].Name)
	}
}

func TestClusterTableSortCycle(t *testing.T) {
	mm := newClusterTestModel(120, 24)

	mm.cycleSort() // → NAME ascending
	if mm.sortCol != colName || !mm.sortAsc {
		t.Fatalf("after first cycle sortCol=%d asc=%v, want NAME asc", mm.sortCol, mm.sortAsc)
	}
	if mm.view[0].Name != "data-platform-production-analytics-cluster-2026" {
		t.Errorf("NAME asc first = %q", mm.view[0].Name)
	}
	if !strings.Contains(mm.tbl.View(), "NAME"+table.SortAscArrow) {
		t.Errorf("header missing NAME sort arrow:\n%s", mm.tbl.View())
	}

	for mm.sortCol != colHRS { // advance to the numeric HRS column
		mm.cycleSort()
	}
	if mm.sortAsc {
		t.Error("HRS should default to descending (most hours first)")
	}
	if mm.view[0].InstanceHours != 128 {
		t.Errorf("HRS desc first = %d hrs, want 128", mm.view[0].InstanceHours)
	}

	// Cycling past the last column returns to the natural order.
	for mm.sortCol != -1 {
		mm.cycleSort()
	}
}

// TestActiveClusterStatesExcludeTerminated guards the default scope: the
// server-side state filter must not list the terminated tail.
func TestActiveClusterStatesExcludeTerminated(t *testing.T) {
	for _, s := range activeClusterStates {
		switch string(s) {
		case "TERMINATED", "TERMINATED_WITH_ERRORS":
			t.Errorf("activeClusterStates must not include terminal state %q", s)
		}
	}
}

// TestTerminatedToggleReloads checks the "t" key flips the scope and kicks off a
// reload (the scope changes what ListClusters returns, so it can't be filtered
// client-side).
func TestTerminatedToggleReloads(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	if mm.showTerminated {
		t.Fatal("dashboard should default to active clusters only")
	}
	cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if !mm.showTerminated {
		t.Error("t should toggle showTerminated on")
	}
	if !mm.loading {
		t.Error("t should trigger a reload (loading=true)")
	}
	if len(cmds) == 0 {
		t.Error("t should issue a reload command")
	}
	// The toggle is guarded while a load is in flight; let it complete, then a
	// second t flips the scope back.
	mm.loading = false
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if mm.showTerminated {
		t.Error("second t should toggle showTerminated back off")
	}
}

// TestEMRFindingsPanel checks the findings panel computes deterministic
// findings over the loaded inventory (the terminated-with-errors cluster is the
// loudest signal) and renders without overflowing the terminal.
func TestEMRFindingsPanel(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.openFindings()
	if !mm.findingsActive {
		t.Fatal("openFindings should activate the panel")
	}
	var gotCrit bool
	for _, f := range mm.findingList {
		if f.Severity == findings.SevCritical && f.Resource == "etl" {
			gotCrit = true
		}
	}
	if !gotCrit {
		t.Errorf("expected a CRITICAL finding for the terminated-with-errors cluster, got %+v", mm.findingList)
	}
	out := mm.View()
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("findings line %d overflows (%d > 120): %q", i, lw, line)
		}
	}
	if !strings.Contains(out, "Findings") {
		t.Errorf("findings view missing heading:\n%s", out)
	}
}

// TestEMRStatusBarPinnedToBottom guards issue #237: when the cluster list has
// no data the status bar must stay on the bottom line of the terminal rather
// than floating up behind the short "no results" message.
func TestEMRStatusBarPinnedToBottom(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv = Inventory{} // no clusters
	mm.rebuild()

	out := mm.View()
	if !strings.Contains(out, "No clusters found in scope.") {
		t.Fatalf("expected the empty-list message:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != mm.height {
		t.Errorf("rendered %d lines, want %d (status bar should fill to the bottom)", len(lines), mm.height)
	}
	if last := lines[len(lines)-1]; !strings.Contains(last, "quit") {
		t.Errorf("status bar is not on the bottom line; last line = %q\nfull:\n%s", last, out)
	}
}

// TestEMREnrichmentGapSuppressesDetailFindings verifies an un-enriched cluster
// (DescribeCluster denied) does not fire detail-derived findings on its blank
// fields — blank means "unknown", not "none".
func TestEMREnrichmentGapSuppressesDetailFindings(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv.Clusters = []Cluster{
		{Name: "ghost", ID: "j-GHOST", Region: "us-east-1", State: "RUNNING", DetailKnown: false},
	}
	mm.inv.EnrichFailures = 1
	mm.openFindings()
	for _, f := range mm.findingList {
		if f.ID == findings.CheckEMRNoLogURI || f.ID == findings.CheckEMRNoSecurityConf {
			t.Errorf("un-enriched cluster should not fire detail-derived finding %s", f.ID)
		}
	}
}

// TestEMREnrichmentGapWarningRenders verifies the enrichment-gap warning shows
// in the list and that reserving its line keeps the status bar on screen
// (layout chrome accounts for it).
func TestEMREnrichmentGapWarningRenders(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.inv.EnrichFailures = 2
	mm.rebuild()
	out := mm.View()
	if !strings.Contains(out, "could not be enriched") {
		t.Errorf("expected enrichment-gap warning, got:\n%s", out)
	}
	if !strings.Contains(out, "quit") {
		t.Error("status bar missing — the warning line may have clipped it")
	}
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 120 {
			t.Errorf("line %d overflows (%d > 120): %q", i, lw, line)
		}
	}
}

// TestEMRDetailOverlayScrollsAndCloses verifies the cluster-describe overlay
// shows a loading state, renders the loaded description (scrollable when taller
// than the viewport), and closes on Esc.
func TestEMRDetailOverlayScrollsAndCloses(t *testing.T) {
	mm := newClusterTestModel(100, 16) // short height → detail taller than the viewport
	cl, _ := mm.selectedCluster()
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if !mm.detailActive {
		t.Fatal("d should open the describe overlay")
	}
	if !mm.descLoading {
		t.Error("d should start the asynchronous describe load")
	}
	if out := mm.View(); !strings.Contains(out, "Describing") {
		t.Errorf("overlay should show a loading state before the describe arrives:\n%s", out)
	}

	// Deliver the loaded description for the selected cluster.
	mm.Update(descMsg{cluster: cl, desc: richTestDescription(cl)})
	if mm.descLoading {
		t.Error("descMsg should clear the loading state")
	}

	out := mm.View() // sizes and fills the viewport
	if !strings.Contains(out, "Describe — "+cl.Name) {
		t.Errorf("describe overlay missing title:\n%s", out)
	}
	// The networking section is below the fold; assert it is in the full body.
	if body := mm.detailBody(); !strings.Contains(body, "Networking") || !strings.Contains(body, "sg-master") {
		t.Errorf("describe body missing the networking section:\n%s", body)
	}
	if mm.detailVP.TotalLineCount() <= mm.detailVP.Height {
		t.Fatalf("test needs content taller than the viewport (lines=%d height=%d)",
			mm.detailVP.TotalLineCount(), mm.detailVP.Height)
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if mm.detailVP.YOffset == 0 {
		t.Error("j should scroll the detail viewport down")
	}
	for i, line := range strings.Split(out, "\n") {
		if lw := ansi.StringWidth(line); lw > 100 {
			t.Errorf("overlay line %d overflows (%d > 100): %q", i, lw, line)
		}
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.detailActive {
		t.Error("Esc should close the describe overlay")
	}
}

// richTestDescription returns a populated ClusterDescription for cl, with enough
// content (multiple groups, instances and full networking) to exceed a short
// viewport.
func richTestDescription(cl Cluster) ClusterDescription {
	yes := true
	return ClusterDescription{
		Cluster:          cl,
		ReleaseLabel:     "emr-7.1.0",
		OSReleaseLabel:   "2.0.20240131.0",
		EbsRootVolumeGiB: 50,
		TerminationProt:  &yes,
		Applications:     []AppInfo{{Name: "Spark", Version: "3.5.0"}, {Name: "Hive", Version: "3.1.3"}},
		Groups: []NodeGroup{
			{Role: "MASTER", InstanceType: "m5.xlarge", Market: "ON_DEMAND", Requested: 1, Running: 1,
				VCPUs: 4, MemoryMiB: 16384, Architecture: "x86_64", SpecsKnown: true,
				EBSVolumes: []EBSVolume{{Device: "/dev/sdb", VolumeType: "gp3", SizeGiB: 64}}},
			{Role: "CORE", InstanceType: "r5.2xlarge", Market: "SPOT", Requested: 4, Running: 3,
				VCPUs: 8, MemoryMiB: 65536, Architecture: "x86_64", SpecsKnown: true,
				EBSVolumes: []EBSVolume{{Device: "/dev/sdb", VolumeType: "gp3", SizeGiB: 256, Iops: 3000}}},
		},
		Instances: []Instance{
			{EC2ID: "i-0aaa", Type: "m5.xlarge", State: "RUNNING", PrivateDNS: "ip-10-0-1-10.ec2.internal"},
			{EC2ID: "i-0bbb", Type: "r5.2xlarge", State: "RUNNING", PrivateDNS: "ip-10-0-1-11.ec2.internal"},
		},
		Network: NetworkInfo{
			SubnetID: "subnet-123", VPCID: "vpc-abc", CIDR: "10.0.1.0/24", AZ: "us-east-1a",
			MapPublicIP: &yes, SubnetKnown: true,
			SecurityGroups: []SecurityGroupRef{
				{ID: "sg-master", Name: "ElasticMapReduce-master", Kind: "EMR-managed (primary)", Known: true,
					Rules: []SGRule{{Direction: "inbound", Protocol: "tcp", Ports: "8088", Source: "10.0.0.0/16"}}},
			},
			RouteTableID: "rtb-1", Routes: []RouteEntry{{Destination: "0.0.0.0/0", Target: "igw-1", State: "active"}},
			NaclID:      "acl-1",
			NaclEntries: []NaclEntry{{Direction: "inbound", RuleNumber: 100, Protocol: "all", Ports: "all", CIDR: "0.0.0.0/0", Action: "allow"}},
		},
	}
}

// TestEMRRefreshGuardedWhileLoading verifies r (and the t toggle) are no-ops
// while a load is already running, so they can't fire concurrent inventory
// loads.
func TestEMRRefreshGuardedWhileLoading(t *testing.T) {
	mm := newClusterTestModel(120, 24)

	mm.loading = true
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) != 0 {
		t.Errorf("r during a load should not start another (got %d cmds)", len(cmds))
	}
	before := mm.showTerminated
	cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if mm.showTerminated != before {
		t.Error("t during a load should not toggle the terminated scope")
	}
	if len(cmds) != 0 {
		t.Error("t during a load should not start a reload")
	}

	mm.loading = false
	if cmds := mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); len(cmds) == 0 || !mm.loading {
		t.Error("r when idle should start a reload")
	}
}

// TestEMRAgeColumnAndSort checks the AGE column renders a compact age and that
// sorting by AGE defaults to oldest-first.
func TestEMRAgeColumnAndSort(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	now := time.Now()
	mm.inv.Clusters = []Cluster{
		{Name: "old", ID: "j-OLD", Region: "us-east-1", State: "RUNNING", Created: now.Add(-72 * time.Hour)},
		{Name: "young", ID: "j-YNG", Region: "us-east-1", State: "RUNNING", Created: now.Add(-1 * time.Hour)},
	}
	mm.rebuild()
	if !strings.Contains(mm.tbl.View(), "3d") {
		t.Errorf("expected AGE 3d for the old cluster:\n%s", mm.tbl.View())
	}

	for mm.sortCol != colAge {
		mm.cycleSort()
	}
	if mm.sortAsc {
		t.Error("AGE should default to descending (oldest first)")
	}
	if mm.view[0].Name != "old" {
		t.Errorf("AGE desc first = %q, want old", mm.view[0].Name)
	}
}

// TestEMRHBaseBoundToB verifies the HBase browser opens on b (not h, which is
// vim-left/back), and that h closes the sub-view.
func TestEMRHBaseBoundToB(t *testing.T) {
	mm := newClusterTestModel(120, 24)

	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if mm.hbaseActive {
		t.Error("h must not open the HBase browser from the cluster list")
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	if !mm.hbaseActive {
		t.Fatal("b should open the HBase browser")
	}
	mm.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	if mm.hbaseActive {
		t.Error("h should go back from (close) the HBase sub-view")
	}
}

// TestEMRProgressiveLoad checks the two-phase load: the skeleton renders and
// schedules enrichment, an enrichMsg patches the matching cluster, and a
// stale-generation enrichMsg is ignored.
func TestEMRProgressiveLoad(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.loadGen = 1
	mm.loading = true
	mm.inv = Inventory{}

	// Phase 1: the skeleton arrives.
	_, cmd := mm.Update(invMsg{gen: 1, inv: Inventory{Clusters: []Cluster{
		{ID: "j-1", Name: "a", Region: "us-east-1", State: "RUNNING"},
	}}})
	if mm.loading {
		t.Error("the skeleton should clear loading")
	}
	if len(mm.view) != 1 {
		t.Fatalf("skeleton not rendered: %d rows", len(mm.view))
	}
	if mm.enrichPending != 1 {
		t.Errorf("enrichPending = %d, want 1 region", mm.enrichPending)
	}
	if cmd == nil {
		t.Error("phase 1 should schedule phase-2 enrichment")
	}

	// Phase 2: enrichment patches the cluster and clears the pending counter.
	enriched := mm.inv.Clusters[0]
	enriched.ReleaseLabel = "emr-7.1.0"
	enriched.DetailKnown = true
	mm.Update(enrichMsg{gen: 1, region: "us-east-1", clusters: []Cluster{enriched}})
	if mm.inv.Clusters[0].ReleaseLabel != "emr-7.1.0" {
		t.Errorf("enrichment not applied: %+v", mm.inv.Clusters[0])
	}
	if mm.enrichPending != 0 {
		t.Errorf("enrichPending = %d after enrich, want 0", mm.enrichPending)
	}

	// A straggler from a superseded load (wrong generation) is ignored.
	stale := mm.inv.Clusters[0]
	stale.ReleaseLabel = "STALE"
	mm.Update(enrichMsg{gen: 0, region: "us-east-1", clusters: []Cluster{stale}})
	if mm.inv.Clusters[0].ReleaseLabel != "emr-7.1.0" {
		t.Error("stale-generation enrichMsg should be ignored")
	}
}

func TestClusterTableFilter(t *testing.T) {
	mm := newClusterTestModel(120, 24)
	mm.filter.SetValue("terminated")
	mm.rebuild()
	if len(mm.view) != 1 || mm.view[0].Name != "etl" {
		t.Errorf("filter 'terminated' => %d rows %+v", len(mm.view), mm.view)
	}
}
