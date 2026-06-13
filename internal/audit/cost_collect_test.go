package audit

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
)

func TestMapCostVolume(t *testing.T) {
	v := ec2types.Volume{
		VolumeId:   aws.String("vol-1"),
		Size:       aws.Int32(100),
		VolumeType: ec2types.VolumeTypeGp2,
		State:      ec2types.VolumeStateAvailable,
	}
	got := mapCostVolume(v)
	if got.ID != "vol-1" || got.Type != "gp2" || got.SizeGiB != 100 || got.State != "available" || got.Attached {
		t.Errorf("mapCostVolume = %+v", got)
	}

	v.Attachments = []ec2types.VolumeAttachment{{InstanceId: aws.String("i-1")}}
	v.State = ec2types.VolumeStateInUse
	if got := mapCostVolume(v); !got.Attached {
		t.Error("volume with attachments should map Attached=true")
	}
}

func TestMapCostAddress(t *testing.T) {
	idle := mapCostAddress(ec2types.Address{
		AllocationId: aws.String("eipalloc-1"),
		PublicIp:     aws.String("52.1.1.1"),
	})
	if idle.Associated || idle.ID != "eipalloc-1" || idle.PublicIP != "52.1.1.1" {
		t.Errorf("idle address = %+v", idle)
	}
	used := mapCostAddress(ec2types.Address{
		AllocationId:  aws.String("eipalloc-2"),
		AssociationId: aws.String("eipassoc-1"),
	})
	if !used.Associated {
		t.Error("address with AssociationId should map Associated=true")
	}
}

func TestMapCostNatGateway(t *testing.T) {
	got := mapCostNatGateway(ec2types.NatGateway{
		NatGatewayId: aws.String("nat-1"),
		State:        ec2types.NatGatewayStateAvailable,
		Tags: []ec2types.Tag{
			{Key: aws.String("env"), Value: aws.String("prod")},
			{Key: aws.String("Name"), Value: aws.String("my-nat")},
		},
	})
	if got.ID != "nat-1" || got.State != "available" || got.Name != "my-nat" {
		t.Errorf("mapCostNatGateway = %+v", got)
	}
}

func TestAddNatRouteRefs(t *testing.T) {
	refs := map[string]bool{}
	addNatRouteRefs(refs, ec2types.RouteTable{
		Routes: []ec2types.Route{
			{NatGatewayId: aws.String("nat-1")},
			{GatewayId: aws.String("igw-1")},
			{NatGatewayId: aws.String("nat-2")},
		},
	})
	if !refs["nat-1"] || !refs["nat-2"] || len(refs) != 2 {
		t.Errorf("refs = %v", refs)
	}
}

func TestMapCostInstance(t *testing.T) {
	got := mapCostInstance(ec2types.Instance{
		InstanceId: aws.String("i-1"),
		ImageId:    aws.String("ami-1"),
		State:      &ec2types.InstanceState{Name: ec2types.InstanceStateNameStopped},
		Tags:       []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("web")}},
		BlockDeviceMappings: []ec2types.InstanceBlockDeviceMapping{
			{Ebs: &ec2types.EbsInstanceBlockDevice{VolumeId: aws.String("vol-1")}},
			{Ebs: nil},
			{Ebs: &ec2types.EbsInstanceBlockDevice{VolumeId: aws.String("vol-2")}},
		},
	})
	if got.ID != "i-1" || got.State != "stopped" || got.Name != "web" || got.ImageID != "ami-1" {
		t.Errorf("mapCostInstance = %+v", got)
	}
	if len(got.VolumeIDs) != 2 || got.VolumeIDs[0] != "vol-1" || got.VolumeIDs[1] != "vol-2" {
		t.Errorf("VolumeIDs = %v", got.VolumeIDs)
	}
}

func TestMapCostImage(t *testing.T) {
	got := mapCostImage(ec2types.Image{
		ImageId:      aws.String("ami-1"),
		Name:         aws.String("build-42"),
		CreationDate: aws.String("2024-01-15T10:30:00.000Z"),
		BlockDeviceMappings: []ec2types.BlockDeviceMapping{
			{Ebs: &ec2types.EbsBlockDevice{SnapshotId: aws.String("snap-1")}},
			{VirtualName: aws.String("ephemeral0")},
		},
	})
	if got.ID != "ami-1" || got.Name != "build-42" {
		t.Errorf("mapCostImage = %+v", got)
	}
	if got.Created.IsZero() || got.Created.Year() != 2024 {
		t.Errorf("Created = %v", got.Created)
	}
	if len(got.SnapshotIDs) != 1 || got.SnapshotIDs[0] != "snap-1" {
		t.Errorf("SnapshotIDs = %v", got.SnapshotIDs)
	}

	// Unparsable creation date leaves Created zero (exempt from age checks).
	bad := mapCostImage(ec2types.Image{ImageId: aws.String("ami-2"), CreationDate: aws.String("not-a-date")})
	if !bad.Created.IsZero() {
		t.Errorf("bad date should leave Created zero, got %v", bad.Created)
	}
}

func TestCountTargetHealth(t *testing.T) {
	ths := []elbv2types.TargetHealthDescription{
		{TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy}},
		{TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumUnhealthy}},
		{TargetHealth: nil},
		{TargetHealth: &elbv2types.TargetHealth{State: elbv2types.TargetHealthStateEnumHealthy}},
	}
	total, healthy := countTargetHealth(ths)
	if total != 4 || healthy != 2 {
		t.Errorf("total/healthy = %d/%d, want 4/2", total, healthy)
	}
}

func TestLBMetricDimension(t *testing.T) {
	dim, ok := lbMetricDimension("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/abc123")
	if !ok || dim != "app/my-alb/abc123" {
		t.Errorf("dim = %q ok = %v", dim, ok)
	}
	if _, ok := lbMetricDimension("arn:aws:ec2:us-east-1:123456789012:instance/i-1"); ok {
		t.Error("non-LB ARN should not yield a dimension")
	}
	if _, ok := lbMetricDimension(""); ok {
		t.Error("empty ARN should not yield a dimension")
	}
}

func TestBuildCostMetricQueries(t *testing.T) {
	snap := &findings.CostSnapshot{
		LoadBalancers: []findings.CostLoadBalancer{
			{ARN: "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/app/alb/1", Type: "application"},
			{ARN: "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/net/nlb/2", Type: "network"},
			{ARN: "arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/gwy/glb/3", Type: "gateway"}, // skipped
		},
		Tables: []findings.CostTable{
			{Name: "provisioned", ProvisionedRCU: 100, ProvisionedWCU: 10},
			{Name: "ondemand"}, // skipped
		},
	}

	queries, bind := buildCostMetricQueries(snap)
	// 2 LB queries (gateway skipped) + 2 table queries (read & write).
	if len(queries) != 4 {
		t.Fatalf("queries = %d, want 4", len(queries))
	}

	// Bind resolved sums and per-id maxes; IDs missing from a map mean zero
	// datapoints.
	bind(
		map[string]float64{
			"lb0": 12345,
			"tr0": 3628800, // 3 RCU avg over 14 days (3 * 14 * 86400)
		},
		map[string]float64{
			"tr0": 36000, // busiest hour: 36000 units => 10 RCU/s peak (36000 / 3600)
		},
	)

	if got := snap.LoadBalancers[0].Requests14d; got == nil || *got != 12345 {
		t.Errorf("ALB Requests14d = %v, want 12345", got)
	}
	if got := snap.LoadBalancers[1].Requests14d; got == nil || *got != 0 {
		t.Errorf("NLB Requests14d = %v, want 0 (no datapoints = idle)", got)
	}
	if got := snap.LoadBalancers[2].Requests14d; got != nil {
		t.Errorf("gateway LB Requests14d = %v, want nil (not queried)", got)
	}
	if got := snap.Tables[0].AvgConsumedRCU; got == nil || *got != 3 {
		t.Errorf("AvgConsumedRCU = %v, want 3", got)
	}
	if got := snap.Tables[0].AvgConsumedWCU; got == nil || *got != 0 {
		t.Errorf("AvgConsumedWCU = %v, want 0", got)
	}
	if got := snap.Tables[0].PeakConsumedRCU; got == nil || *got != 10 {
		t.Errorf("PeakConsumedRCU = %v, want 10 (36000 busiest-hour units / 3600s)", got)
	}
	if got := snap.Tables[0].PeakConsumedWCU; got == nil || *got != 0 {
		t.Errorf("PeakConsumedWCU = %v, want 0 (no datapoints)", got)
	}
	if snap.Tables[1].AvgConsumedRCU != nil {
		t.Error("on-demand table should not be queried")
	}
}

func TestErrRecorderClassifiesErrors(t *testing.T) {
	rec := &errRecorder{region: "us-east-1"}
	rec.record("ec2", errors.New("connection reset"))
	rec.record("ec2", nil) // no-op
	if len(rec.errs) != 1 {
		t.Fatalf("errs = %d, want 1", len(rec.errs))
	}
	e := rec.errs[0]
	if e.Service != "ec2" || e.Region != "us-east-1" || e.Code != "CollectionError" {
		t.Errorf("recorded error = %+v", e)
	}
}

func TestWithTimeout(t *testing.T) {
	ctx, cancel := withTimeout(t.Context(), 0)
	defer cancel()
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Error("zero timeout should not set a deadline")
	}
	ctx2, cancel2 := withTimeout(t.Context(), time.Minute)
	defer cancel2()
	if _, hasDeadline := ctx2.Deadline(); !hasDeadline {
		t.Error("positive timeout should set a deadline")
	}
}
