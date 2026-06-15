package costs

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func TestGlueRunCost(t *testing.T) {
	// 7416 DPU-seconds = 2.06 DPU-hours; at $0.44/DPU-hr ≈ $0.9064.
	if !approx(GlueRunDPUHours(7416), 2.06) {
		t.Errorf("GlueRunDPUHours(7416) = %v, want ≈2.06", GlueRunDPUHours(7416))
	}
	if !approx(GlueRunCostUSD(7416), 2.06*GluePerDPUHourUSD) {
		t.Errorf("GlueRunCostUSD(7416) = %v", GlueRunCostUSD(7416))
	}
	// Absent DPUSeconds (running/legacy run) yields no estimate, not a negative.
	if GlueRunDPUHours(0) != 0 || GlueRunCostUSD(0) != 0 {
		t.Errorf("zero DPUSeconds should yield zero, got hrs=%v cost=%v", GlueRunDPUHours(0), GlueRunCostUSD(0))
	}
}

func TestEBSPerGiBMonth(t *testing.T) {
	cases := map[string]float64{
		"gp2":      EBSGP2PerGiBMonth,
		"gp3":      EBSGP3PerGiBMonth,
		"io1":      EBSIO1PerGiBMonth,
		"io2":      EBSIO2PerGiBMonth,
		"st1":      EBSST1PerGiBMonth,
		"sc1":      EBSSC1PerGiBMonth,
		"standard": EBSMagneticPerGiBMonth,
		// Unknown types fall back to the gp3 rate (conservative).
		"gp4-future": EBSGP3PerGiBMonth,
	}
	for typ, want := range cases {
		if got := EBSPerGiBMonth(typ); !approx(got, want) {
			t.Errorf("EBSPerGiBMonth(%q) = %v, want %v", typ, got, want)
		}
	}
}

func TestEBSVolumeMonth(t *testing.T) {
	if got := EBSVolumeMonth("gp2", 100); !approx(got, 10.0) {
		t.Errorf("100 GiB gp2 = %v, want 10.0", got)
	}
	if got := EBSVolumeMonth("gp2", 0); got != 0 {
		t.Errorf("zero size = %v, want 0", got)
	}
	if got := EBSVolumeMonth("gp2", -5); got != 0 {
		t.Errorf("negative size = %v, want 0", got)
	}
}

func TestGP2ToGP3SavingsMonth(t *testing.T) {
	if got := GP2ToGP3SavingsMonth(1000); !approx(got, 20.0) {
		t.Errorf("1000 GiB savings = %v, want 20.0", got)
	}
}

func TestSnapshotMonth(t *testing.T) {
	if got := SnapshotMonth(200); !approx(got, 10.0) {
		t.Errorf("200 GiB snapshot = %v, want 10.0", got)
	}
}

func TestLoadBalancerMonth(t *testing.T) {
	if got := LoadBalancerMonth("application"); !approx(got, ALBMonth) {
		t.Errorf("application = %v, want %v", got, ALBMonth)
	}
	if got := LoadBalancerMonth("network"); !approx(got, NLBMonth) {
		t.Errorf("network = %v, want %v", got, NLBMonth)
	}
	if got := LoadBalancerMonth("gateway"); !approx(got, GWLBMonth) {
		t.Errorf("gateway = %v, want %v", got, GWLBMonth)
	}
}

func TestDynamoDBProvisionedMonth(t *testing.T) {
	want := 100*DynamoDBRCUMonth + 50*DynamoDBWCUMonth
	if got := DynamoDBProvisionedMonth(100, 50); !approx(got, want) {
		t.Errorf("100 RCU / 50 WCU = %v, want %v", got, want)
	}
	if got := DynamoDBProvisionedMonth(-1, -1); got != 0 {
		t.Errorf("negative capacity = %v, want 0", got)
	}
}

func TestDollars(t *testing.T) {
	if got := Dollars(32.85); got != "$32.85" {
		t.Errorf("Dollars(32.85) = %q", got)
	}
	if got := Dollars(0); got != "-" {
		t.Errorf("Dollars(0) = %q, want -", got)
	}
	if got := Dollars(-1); got != "-" {
		t.Errorf("Dollars(-1) = %q, want -", got)
	}
}
