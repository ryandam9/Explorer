package findings

import (
	"strings"
	"testing"
	"time"

	"github.com/ryandam9/aws_explorer/internal/costs"
)

var testNow = time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

func baseSnap() CostSnapshot {
	return CostSnapshot{Region: "us-east-1", Now: testNow, NatRouteRefs: map[string]bool{}, InstancesComplete: true}
}

// findByID returns the findings produced by one check.
func findByID(fs []Finding, id string) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.ID == id {
			out = append(out, f)
		}
	}
	return out
}

func f64(v float64) *float64 { return &v }

func TestUnattachedVolume(t *testing.T) {
	snap := baseSnap()
	snap.Volumes = []CostVolume{
		{ID: "vol-idle", Type: "gp2", SizeGiB: 1024, State: "available"},
		{ID: "vol-used", Type: "gp2", SizeGiB: 100, State: "in-use", Attached: true},
	}
	fs := AnalyzeCost(snap)

	got := findByID(fs, CheckUnattachedVolume)
	if len(got) != 1 {
		t.Fatalf("unattached findings = %d, want 1", len(got))
	}
	f := got[0]
	if f.Resource != "vol-idle" {
		t.Errorf("Resource = %q", f.Resource)
	}
	if f.Severity != SevWarning {
		t.Errorf("Severity = %v, want warning", f.Severity)
	}
	if want := costs.EBSVolumeMonth("gp2", 1024); f.EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", f.EstMonthlyUSD, want)
	}
	// The unattached gp2 volume must NOT also get a gp2→gp3 suggestion;
	// the attached one must.
	gp2 := findByID(fs, CheckGP2Volume)
	if len(gp2) != 1 || gp2[0].Resource != "vol-used" {
		t.Errorf("gp2 findings = %+v, want exactly vol-used", gp2)
	}
}

func TestGP2Volume(t *testing.T) {
	snap := baseSnap()
	snap.Volumes = []CostVolume{
		{ID: "vol-a", Type: "gp2", SizeGiB: 500, State: "in-use", Attached: true},
		{ID: "vol-b", Type: "gp3", SizeGiB: 500, State: "in-use", Attached: true},
	}
	got := findByID(AnalyzeCost(snap), CheckGP2Volume)
	if len(got) != 1 {
		t.Fatalf("gp2 findings = %d, want 1", len(got))
	}
	if want := costs.GP2ToGP3SavingsMonth(500); got[0].EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, want)
	}
	if got[0].Severity != SevInfo {
		t.Errorf("Severity = %v, want info", got[0].Severity)
	}
}

func TestUnassociatedEIP(t *testing.T) {
	snap := baseSnap()
	snap.Addresses = []CostAddress{
		{ID: "eipalloc-idle", PublicIP: "52.1.1.1", Associated: false},
		{ID: "eipalloc-used", PublicIP: "52.1.1.2", Associated: true},
	}
	got := findByID(AnalyzeCost(snap), CheckUnassociatedEIP)
	if len(got) != 1 {
		t.Fatalf("EIP findings = %d, want 1", len(got))
	}
	if got[0].Resource != "eipalloc-idle" {
		t.Errorf("Resource = %q", got[0].Resource)
	}
	if got[0].EstMonthlyUSD != costs.ElasticIPMonth {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, costs.ElasticIPMonth)
	}
	if !strings.Contains(got[0].Detail, "52.1.1.1") {
		t.Errorf("Detail should name the public IP: %q", got[0].Detail)
	}
}

func TestIdleNATGateway(t *testing.T) {
	snap := baseSnap()
	snap.NatGateways = []CostNatGateway{
		{ID: "nat-idle", Name: "orphan", State: "available"},
		{ID: "nat-used", State: "available"},
		{ID: "nat-gone", State: "deleted"},
	}
	snap.NatRouteRefs = map[string]bool{"nat-used": true}

	got := findByID(AnalyzeCost(snap), CheckIdleNATGateway)
	if len(got) != 1 {
		t.Fatalf("NAT findings = %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Resource, "nat-idle") || !strings.Contains(got[0].Resource, "orphan") {
		t.Errorf("Resource = %q, want id and name", got[0].Resource)
	}
	if got[0].EstMonthlyUSD != costs.NATGatewayMonth {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, costs.NATGatewayMonth)
	}
}

func TestLBNoHealthyTargets(t *testing.T) {
	old := testNow.Add(-60 * 24 * time.Hour)
	snap := baseSnap()
	snap.LoadBalancers = []CostLoadBalancer{
		{ARN: "arn:lb/unhealthy", Name: "unhealthy", Type: "application", Created: old,
			TargetGroups: 2, TotalTargets: 3, HealthyTargets: 0, HealthKnown: true, Requests14d: f64(0)},
		{ARN: "arn:lb/empty", Name: "empty", Type: "network", Created: old,
			TargetGroups: 1, TotalTargets: 0, HealthyTargets: 0, HealthKnown: true},
		{ARN: "arn:lb/healthy", Name: "healthy", Type: "application", Created: old,
			TargetGroups: 1, TotalTargets: 2, HealthyTargets: 2, HealthKnown: true, Requests14d: f64(1000)},
		{ARN: "arn:lb/unknown", Name: "unknown", Type: "application", Created: old,
			TargetGroups: 1, HealthKnown: false},
		{ARN: "arn:lb/no-tg", Name: "no-tg", Type: "application", Created: old,
			TargetGroups: 0, HealthKnown: true},
	}
	fs := AnalyzeCost(snap)

	got := findByID(fs, CheckLBNoHealthyTarget)
	if len(got) != 2 {
		t.Fatalf("zero-healthy findings = %d, want 2 (%+v)", len(got), got)
	}
	names := got[0].Resource + " " + got[1].Resource
	if !strings.Contains(names, "unhealthy") || !strings.Contains(names, "empty") {
		t.Errorf("flagged = %q, want unhealthy and empty", names)
	}
	// The unhealthy LB has zero traffic too, but must not be double-counted
	// as idle.
	if idle := findByID(fs, CheckLBIdle); len(idle) != 0 {
		t.Errorf("idle findings = %+v, want none", idle)
	}
}

func TestLBIdle(t *testing.T) {
	old := testNow.Add(-60 * 24 * time.Hour)
	recent := testNow.Add(-2 * 24 * time.Hour)
	snap := baseSnap()
	snap.LoadBalancers = []CostLoadBalancer{
		// Idle: healthy targets but no traffic.
		{ARN: "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/idle/1", Name: "idle", Type: "network", Created: old,
			TargetGroups: 1, TotalTargets: 1, HealthyTargets: 1, HealthKnown: true, Requests14d: f64(0)},
		// Busy.
		{ARN: "arn:lb/busy", Name: "busy", Type: "application", Created: old,
			TargetGroups: 1, TotalTargets: 1, HealthyTargets: 1, HealthKnown: true, Requests14d: f64(5)},
		// Metrics unknown: skipped.
		{ARN: "arn:lb/nometrics", Name: "nometrics", Type: "application", Created: old,
			TargetGroups: 1, TotalTargets: 1, HealthyTargets: 1, HealthKnown: true, Requests14d: nil},
		// Too new for a 14-day verdict: skipped.
		{ARN: "arn:lb/new", Name: "new", Type: "application", Created: recent,
			TargetGroups: 1, TotalTargets: 1, HealthyTargets: 1, HealthKnown: true, Requests14d: f64(0)},
	}
	got := findByID(AnalyzeCost(snap), CheckLBIdle)
	if len(got) != 1 {
		t.Fatalf("idle findings = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Resource != "idle" {
		t.Errorf("Resource = %q, want idle", got[0].Resource)
	}
	if !strings.Contains(got[0].Detail, "new flows") {
		t.Errorf("NLB detail should mention flows: %q", got[0].Detail)
	}
	if got[0].EstMonthlyUSD != costs.NLBMonth {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, costs.NLBMonth)
	}
}

func TestStoppedInstanceWithEBS(t *testing.T) {
	snap := baseSnap()
	snap.Volumes = []CostVolume{
		{ID: "vol-1", Type: "gp3", SizeGiB: 100, State: "in-use", Attached: true},
		{ID: "vol-2", Type: "gp2", SizeGiB: 50, State: "in-use", Attached: true},
	}
	snap.Instances = []CostInstance{
		{ID: "i-stopped", Name: "batch", State: "stopped", VolumeIDs: []string{"vol-1", "vol-2"}},
		{ID: "i-running", State: "running", VolumeIDs: []string{"vol-1"}},
		{ID: "i-novols", State: "stopped"},
		// Volume data missing entirely: no zero-dollar noise.
		{ID: "i-unknownvol", State: "stopped", VolumeIDs: []string{"vol-x"}},
	}
	got := findByID(AnalyzeCost(snap), CheckStoppedWithEBS)
	if len(got) != 1 {
		t.Fatalf("stopped findings = %d, want 1 (%+v)", len(got), got)
	}
	want := costs.EBSVolumeMonth("gp3", 100) + costs.EBSVolumeMonth("gp2", 50)
	if got[0].EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, want)
	}
	if !strings.Contains(got[0].Resource, "i-stopped") {
		t.Errorf("Resource = %q", got[0].Resource)
	}
}

func TestOldSnapshot(t *testing.T) {
	old := testNow.Add(-200 * 24 * time.Hour)
	recent := testNow.Add(-30 * 24 * time.Hour)
	snap := baseSnap()
	snap.Snapshots = []CostEBSSnapshot{
		{ID: "snap-old", SizeGiB: 200, Started: old},
		{ID: "snap-recent", SizeGiB: 200, Started: recent},
		{ID: "snap-ami", SizeGiB: 200, Started: old}, // backs an AMI
		{ID: "snap-nodate", SizeGiB: 200},            // unknown age: skipped
	}
	snap.Images = []CostImage{
		{ID: "ami-1", Created: recent, SnapshotIDs: []string{"snap-ami"}},
	}
	snap.Instances = []CostInstance{{ID: "i-1", State: "running", ImageID: "ami-1"}}

	got := findByID(AnalyzeCost(snap), CheckOldSnapshot)
	if len(got) != 1 {
		t.Fatalf("snapshot findings = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].Resource != "snap-old" {
		t.Errorf("Resource = %q, want snap-old", got[0].Resource)
	}
	if want := costs.SnapshotMonth(200); got[0].EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, want)
	}
	if !strings.Contains(got[0].Title, "200 days") {
		t.Errorf("Title should state the age: %q", got[0].Title)
	}
}

func TestUnusedAMI(t *testing.T) {
	old := testNow.Add(-365 * 24 * time.Hour)
	snap := baseSnap()
	snap.Snapshots = []CostEBSSnapshot{
		{ID: "snap-a", SizeGiB: 100, Started: old},
		{ID: "snap-b", SizeGiB: 50, Started: old},
	}
	snap.Images = []CostImage{
		{ID: "ami-unused", Name: "old-build", Created: old, SnapshotIDs: []string{"snap-a", "snap-b"}},
		{ID: "ami-used", Created: old, SnapshotIDs: []string{"snap-a"}},
		{ID: "ami-new", Created: testNow.Add(-24 * time.Hour)},
	}
	snap.Instances = []CostInstance{{ID: "i-1", State: "stopped", ImageID: "ami-used"}}

	fs := AnalyzeCost(snap)
	got := findByID(fs, CheckUnusedAMI)
	if len(got) != 1 {
		t.Fatalf("AMI findings = %d, want 1 (%+v)", len(got), got)
	}
	if !strings.Contains(got[0].Resource, "ami-unused") {
		t.Errorf("Resource = %q", got[0].Resource)
	}
	want := costs.SnapshotMonth(100) + costs.SnapshotMonth(50)
	if got[0].EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", got[0].EstMonthlyUSD, want)
	}
	// AMI-backed snapshots are attributed to the AMI finding, never also to
	// the old-snapshot finding.
	if snaps := findByID(fs, CheckOldSnapshot); len(snaps) != 0 {
		t.Errorf("old-snapshot findings = %+v, want none (all back AMIs)", snaps)
	}
}

func TestUnusedAMISkippedOnPartialInstanceListing(t *testing.T) {
	old := testNow.Add(-365 * 24 * time.Hour)
	snap := baseSnap()
	snap.InstancesComplete = false
	snap.Images = []CostImage{{ID: "ami-1", Created: old, SnapshotIDs: []string{"snap-a"}}}
	if got := findByID(AnalyzeCost(snap), CheckUnusedAMI); len(got) != 0 {
		t.Errorf("partial instance listing must not flag AMIs as unused: %+v", got)
	}
}

func TestDDBOverProvisioned(t *testing.T) {
	old := testNow.Add(-90 * 24 * time.Hour)
	snap := baseSnap()
	snap.Tables = []CostTable{
		// 3% utilization: flagged.
		{Name: "cold", Created: old, ProvisionedRCU: 100, ProvisionedWCU: 100,
			AvgConsumedRCU: f64(3), AvgConsumedWCU: f64(3)},
		// 50% utilization: fine.
		{Name: "warm", Created: old, ProvisionedRCU: 100, ProvisionedWCU: 100,
			AvgConsumedRCU: f64(50), AvgConsumedWCU: f64(50)},
		// On-demand: skipped.
		{Name: "ondemand", Created: old, AvgConsumedRCU: f64(0), AvgConsumedWCU: f64(0)},
		// Tiny provision: skipped as noise.
		{Name: "tiny", Created: old, ProvisionedRCU: 5, ProvisionedWCU: 1,
			AvgConsumedRCU: f64(0), AvgConsumedWCU: f64(0)},
		// Metrics unavailable: skipped.
		{Name: "nometrics", Created: old, ProvisionedRCU: 100, ProvisionedWCU: 100},
		// Too new: skipped.
		{Name: "new", Created: testNow.Add(-24 * time.Hour), ProvisionedRCU: 100, ProvisionedWCU: 100,
			AvgConsumedRCU: f64(0), AvgConsumedWCU: f64(0)},
	}
	got := findByID(AnalyzeCost(snap), CheckDDBOverProvision)
	if len(got) != 1 {
		t.Fatalf("DDB findings = %d, want 1 (%+v)", len(got), got)
	}
	f := got[0]
	if f.Resource != "cold" {
		t.Errorf("Resource = %q, want cold", f.Resource)
	}
	provCost := costs.DynamoDBProvisionedMonth(100, 100)
	neededCost := costs.DynamoDBProvisionedMonth(3*ddbHeadroomFactor, 3*ddbHeadroomFactor)
	if want := provCost - neededCost; f.EstMonthlyUSD != want {
		t.Errorf("Est = %v, want %v", f.EstMonthlyUSD, want)
	}
	if !strings.Contains(f.Title, "3%") {
		t.Errorf("Title should state utilization: %q", f.Title)
	}
}

func TestDDBOverProvisioned_PeakSizing(t *testing.T) {
	old := testNow.Add(-90 * 24 * time.Hour)

	// Bursty table: low 14-day average (3 RCU/WCU => flagged on utilization)
	// but peaks at 90/90, near the 100/100 provisioned. Sizing headroom off
	// the peak (90 * 1.x) leaves no real savings, so it must NOT be reported —
	// recommending a downsize here would throttle the table.
	bursty := baseSnap()
	bursty.Tables = []CostTable{{
		Name: "bursty", Created: old, ProvisionedRCU: 100, ProvisionedWCU: 100,
		AvgConsumedRCU: f64(3), AvgConsumedWCU: f64(3),
		PeakConsumedRCU: f64(90), PeakConsumedWCU: f64(90),
	}}
	if got := findByID(AnalyzeCost(bursty), CheckDDBOverProvision); len(got) != 0 {
		t.Errorf("bursty table flagged despite near-peak usage: %+v", got)
	}

	// Genuinely over-provisioned: low average and a modest peak. Savings are
	// sized from the peak, not the (smaller) average.
	idle := baseSnap()
	idle.Tables = []CostTable{{
		Name: "idle", Created: old, ProvisionedRCU: 100, ProvisionedWCU: 100,
		AvgConsumedRCU: f64(3), AvgConsumedWCU: f64(3),
		PeakConsumedRCU: f64(8), PeakConsumedWCU: f64(8),
	}}
	got := findByID(AnalyzeCost(idle), CheckDDBOverProvision)
	if len(got) != 1 {
		t.Fatalf("idle table findings = %d, want 1", len(got))
	}
	provCost := costs.DynamoDBProvisionedMonth(100, 100)
	wantEst := provCost - costs.DynamoDBProvisionedMonth(8*ddbHeadroomFactor, 8*ddbHeadroomFactor)
	if got[0].EstMonthlyUSD != wantEst {
		t.Errorf("Est = %v, want %v (sized from peak)", got[0].EstMonthlyUSD, wantEst)
	}
	if !strings.Contains(got[0].Detail, "Peak observed") {
		t.Errorf("Detail should mention peak: %q", got[0].Detail)
	}
}

func TestAnalyzeCostEmptySnapshot(t *testing.T) {
	if fs := AnalyzeCost(baseSnap()); len(fs) != 0 {
		t.Errorf("empty snapshot produced findings: %+v", fs)
	}
}

func TestAnalyzeCostFindingsCarryRegionAndService(t *testing.T) {
	snap := baseSnap()
	snap.Region = "eu-west-1"
	snap.Volumes = []CostVolume{{ID: "vol-1", Type: "gp2", SizeGiB: 10, State: "available"}}
	fs := AnalyzeCost(snap)
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1", len(fs))
	}
	if fs[0].Region != "eu-west-1" {
		t.Errorf("Region = %q", fs[0].Region)
	}
	if fs[0].Service != "ec2" {
		t.Errorf("Service = %q", fs[0].Service)
	}
}

func TestPercent(t *testing.T) {
	cases := map[float64]string{
		0.03:  "3%",
		0.005: "<1%",
		0.50:  "50%",
		0:     "0%",
	}
	for in, want := range cases {
		if got := percent(in); got != want {
			t.Errorf("percent(%v) = %q, want %q", in, got, want)
		}
	}
}
