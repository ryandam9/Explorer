package findings

import (
	"fmt"
	"strings"
	"time"

	"github.com/ryandam9/aws_explorer/internal/costs"
)

// ---------------------------------------------------------------------------
// Cost/waste linter (AXE-004)
//
// AnalyzeCost inspects a per-region snapshot of billable resources and flags
// the classic sources of silent spend: unattached volumes, idle addresses and
// gateways, load balancers nobody uses, forgotten snapshots/AMIs, and
// over-provisioned DynamoDB capacity. Every check is a pure function over the
// snapshot; data collection lives in internal/audit.
// ---------------------------------------------------------------------------

// Stable check IDs. Never renumber: findings are referenced by these.
const (
	CheckUnattachedVolume  = "COST-EBS-001"
	CheckGP2Volume         = "COST-EBS-002"
	CheckUnassociatedEIP   = "COST-EIP-001"
	CheckIdleNATGateway    = "COST-NAT-001"
	CheckLBNoHealthyTarget = "COST-ELB-001"
	CheckLBIdle            = "COST-ELB-002"
	CheckStoppedWithEBS    = "COST-EC2-001"
	CheckOldSnapshot       = "COST-SNAP-001"
	CheckUnusedAMI         = "COST-AMI-001"
	CheckDDBOverProvision  = "COST-DDB-001"
)

// Tunable thresholds.
const (
	// oldSnapshotAge / unusedImageAge: how old a snapshot/AMI must be before
	// it is considered forgotten.
	oldSnapshotAge = 180 * 24 * time.Hour
	unusedImageAge = 180 * 24 * time.Hour

	// lbIdleWindow is the traffic lookback for the idle load balancer check;
	// load balancers younger than the window are never flagged.
	lbIdleWindow = 14 * 24 * time.Hour

	// ddbLowUtilFrac flags provisioned tables consuming under this fraction
	// of their provisioned capacity; ddbMinProvisionedUnits skips tiny tables
	// whose savings would be noise.
	ddbLowUtilFrac         = 0.10
	ddbMinProvisionedUnits = 10
	// ddbHeadroomFactor pads observed consumption when estimating how much
	// capacity the table actually needs.
	ddbHeadroomFactor = 1.2
)

// CostSnapshot bundles the billable resources of one region. Slices may be
// empty when a resource type could not be fetched (e.g. missing permissions);
// checks over missing data simply produce no findings. Metric-derived fields
// are pointers: nil means "metrics unavailable", which skips the dependent
// check rather than guessing.
type CostSnapshot struct {
	Region string
	Now    time.Time

	Volumes       []CostVolume
	Addresses     []CostAddress
	NatGateways   []CostNatGateway
	NatRouteRefs  map[string]bool // NAT gateway IDs referenced by any route
	LoadBalancers []CostLoadBalancer
	Instances     []CostInstance
	Snapshots     []CostEBSSnapshot
	Images        []CostImage
	Tables        []CostTable

	// InstancesComplete is true when the instance listing finished without
	// error. The unused-AMI check needs the complete set: deciding "no
	// instance uses this image" from a partial listing would flag AMIs whose
	// instances were on a page that failed.
	InstancesComplete bool
}

// CostVolume is an EBS volume.
type CostVolume struct {
	ID       string
	Type     string // gp2, gp3, io1, …
	SizeGiB  int32
	State    string // available, in-use, …
	Attached bool
}

// CostAddress is an Elastic IP / public IPv4 allocation.
type CostAddress struct {
	ID         string // allocation ID
	PublicIP   string
	Associated bool
}

// CostNatGateway is a NAT gateway.
type CostNatGateway struct {
	ID    string
	Name  string
	State string
}

// CostLoadBalancer is an ELBv2 load balancer with its target health and
// (optionally) 14-day traffic.
type CostLoadBalancer struct {
	ARN     string
	Name    string
	Type    string // application, network, gateway
	Created time.Time

	TargetGroups   int
	TotalTargets   int
	HealthyTargets int
	HealthKnown    bool // target health was fetched successfully

	// Requests14d is the request (ALB) / new-flow (NLB) count over the idle
	// window. nil = metrics unavailable or not applicable (gateway LBs).
	Requests14d *float64
}

// CostInstance is an EC2 instance (any state).
type CostInstance struct {
	ID        string
	Name      string
	State     string
	ImageID   string
	VolumeIDs []string
}

// CostEBSSnapshot is a self-owned EBS snapshot.
type CostEBSSnapshot struct {
	ID          string
	SizeGiB     int32
	Started     time.Time
	Description string
}

// CostImage is a self-owned AMI with its backing snapshots.
type CostImage struct {
	ID          string
	Name        string
	Created     time.Time // zero when AWS returned an unparsable date
	SnapshotIDs []string
}

// CostTable is a DynamoDB table.
type CostTable struct {
	Name    string
	ARN     string
	Created time.Time

	// Provisioned capacity; both zero means on-demand billing.
	ProvisionedRCU int64
	ProvisionedWCU int64

	// Average consumed capacity units per second over the lookback window.
	// nil = metrics unavailable.
	AvgConsumedRCU *float64
	AvgConsumedWCU *float64
}

// AnalyzeCost runs every cost/waste check over the snapshot. The result is
// unsorted; callers merge regions and apply Sort once.
func AnalyzeCost(snap CostSnapshot) []Finding {
	var out []Finding
	checkVolumes(snap, &out)
	checkAddresses(snap, &out)
	checkNATGateways(snap, &out)
	checkLoadBalancers(snap, &out)
	checkStoppedInstances(snap, &out)
	checkSnapshotsAndImages(snap, &out)
	checkDynamoDBTables(snap, &out)
	return out
}

// checkVolumes flags unattached volumes and gp2→gp3 migration candidates.
func checkVolumes(snap CostSnapshot, out *[]Finding) {
	for _, v := range snap.Volumes {
		if v.State == "available" && !v.Attached {
			*out = append(*out, Finding{
				ID:            CheckUnattachedVolume,
				Severity:      SevWarning,
				Service:       "ec2",
				Region:        snap.Region,
				Resource:      v.ID,
				Title:         fmt.Sprintf("Unattached EBS volume (%s, %d GiB)", v.Type, v.SizeGiB),
				Detail:        "The volume is not attached to any instance but still bills for its full provisioned size every month.",
				Fix:           "Snapshot the volume and delete it, or attach it to an instance if it is still needed.",
				EstMonthlyUSD: costs.EBSVolumeMonth(v.Type, v.SizeGiB),
			})
			// The delete suggestion supersedes a migration suggestion; don't
			// also tell the user to convert a volume they should remove.
			continue
		}
		if v.Type == "gp2" {
			*out = append(*out, Finding{
				ID:            CheckGP2Volume,
				Severity:      SevInfo,
				Service:       "ec2",
				Region:        snap.Region,
				Resource:      v.ID,
				Title:         fmt.Sprintf("gp2 volume could be gp3 (%d GiB)", v.SizeGiB),
				Detail:        "gp3 costs ~20% less per GiB than gp2 and its 3000 IOPS / 125 MiB/s baseline covers typical gp2 workloads.",
				Fix:           "Modify the volume type to gp3 (an online operation with no downtime).",
				EstMonthlyUSD: costs.GP2ToGP3SavingsMonth(v.SizeGiB),
			})
		}
	}
}

// checkAddresses flags unassociated Elastic IPs.
func checkAddresses(snap CostSnapshot, out *[]Finding) {
	for _, a := range snap.Addresses {
		if a.Associated {
			continue
		}
		res := a.ID
		if res == "" {
			res = a.PublicIP
		}
		detail := "An Elastic IP that is not associated with a running resource still bills hourly."
		if a.PublicIP != "" {
			detail = fmt.Sprintf("Elastic IP %s is not associated with any resource but still bills hourly.", a.PublicIP)
		}
		*out = append(*out, Finding{
			ID:            CheckUnassociatedEIP,
			Severity:      SevWarning,
			Service:       "ec2",
			Region:        snap.Region,
			Resource:      res,
			Title:         "Elastic IP not associated",
			Detail:        detail,
			Fix:           "Release the address, or associate it with the instance/NAT gateway that needs it.",
			EstMonthlyUSD: costs.ElasticIPMonth,
		})
	}
}

// checkNATGateways flags available NAT gateways no route table points at —
// the account-wide generalization of the VPC explorer's idle-NAT check.
func checkNATGateways(snap CostSnapshot, out *[]Finding) {
	for _, n := range snap.NatGateways {
		if n.State != "available" || snap.NatRouteRefs[n.ID] {
			continue
		}
		res := n.ID
		if n.Name != "" {
			res = fmt.Sprintf("%s (%s)", n.ID, n.Name)
		}
		*out = append(*out, Finding{
			ID:            CheckIdleNATGateway,
			Severity:      SevWarning,
			Service:       "ec2",
			Region:        snap.Region,
			Resource:      res,
			Title:         "NAT gateway not referenced by any route",
			Detail:        "The gateway is available but no route table routes through it, so it carries no traffic while billing every hour (plus data processing when used).",
			Fix:           "Delete the NAT gateway, or add the missing route if subnets were meant to use it.",
			EstMonthlyUSD: costs.NATGatewayMonth,
		})
	}
}

// checkLoadBalancers flags LBs whose targets are all unhealthy/unregistered,
// and LBs with zero traffic over the idle window. A zero-healthy LB is not
// also reported idle — it necessarily has no traffic, and double-listing it
// would double-count the estimate.
func checkLoadBalancers(snap CostSnapshot, out *[]Finding) {
	for _, lb := range snap.LoadBalancers {
		monthly := costs.LoadBalancerMonth(lb.Type)

		if lb.HealthKnown && lb.TargetGroups > 0 && lb.HealthyTargets == 0 {
			detail := fmt.Sprintf("None of the %d registered targets across %d target group(s) is healthy; the load balancer cannot serve traffic but bills hourly.", lb.TotalTargets, lb.TargetGroups)
			if lb.TotalTargets == 0 {
				detail = fmt.Sprintf("No targets are registered in its %d target group(s); the load balancer serves nothing but bills hourly.", lb.TargetGroups)
			}
			*out = append(*out, Finding{
				ID:            CheckLBNoHealthyTarget,
				Severity:      SevWarning,
				Service:       "elbv2",
				Region:        snap.Region,
				Resource:      lb.Name,
				ARN:           lb.ARN,
				Title:         fmt.Sprintf("Load balancer (%s) has no healthy targets", lb.Type),
				Detail:        detail,
				Fix:           "Fix target health / register targets, or delete the load balancer if it is no longer used.",
				EstMonthlyUSD: monthly,
			})
			continue
		}

		// Idle check: requires metrics, and an LB old enough to have had its
		// chance at traffic.
		if lb.Requests14d == nil || *lb.Requests14d > 0 {
			continue
		}
		if lb.Created.IsZero() || snap.Now.Sub(lb.Created) < lbIdleWindow {
			continue
		}
		unit := "requests"
		if lb.Type == "network" {
			unit = "new flows"
		}
		*out = append(*out, Finding{
			ID:            CheckLBIdle,
			Severity:      SevWarning,
			Service:       "elbv2",
			Region:        snap.Region,
			Resource:      lb.Name,
			ARN:           lb.ARN,
			Title:         fmt.Sprintf("Load balancer (%s) received no traffic in 14 days", lb.Type),
			Detail:        fmt.Sprintf("CloudWatch recorded 0 %s in the last 14 days; the load balancer bills hourly regardless.", unit),
			Fix:           "Delete the load balancer if it is no longer used.",
			EstMonthlyUSD: monthly,
		})
	}
}

// checkStoppedInstances flags stopped instances whose attached EBS volumes
// keep billing. Severity is informational — stopping is often deliberate —
// but the storage cost is real and worth surfacing.
func checkStoppedInstances(snap CostSnapshot, out *[]Finding) {
	volByID := make(map[string]CostVolume, len(snap.Volumes))
	for _, v := range snap.Volumes {
		volByID[v.ID] = v
	}
	for _, in := range snap.Instances {
		if in.State != "stopped" || len(in.VolumeIDs) == 0 {
			continue
		}
		var est float64
		var sized int
		for _, id := range in.VolumeIDs {
			if v, ok := volByID[id]; ok {
				est += costs.EBSVolumeMonth(v.Type, v.SizeGiB)
				sized++
			}
		}
		if sized == 0 {
			// Volume data unavailable; an estimate of zero would just be noise.
			continue
		}
		res := in.ID
		if in.Name != "" {
			res = fmt.Sprintf("%s (%s)", in.ID, in.Name)
		}
		*out = append(*out, Finding{
			ID:            CheckStoppedWithEBS,
			Severity:      SevInfo,
			Service:       "ec2",
			Region:        snap.Region,
			Resource:      res,
			Title:         fmt.Sprintf("Stopped instance still paying for %d EBS volume(s)", len(in.VolumeIDs)),
			Detail:        "A stopped instance accrues no compute charges, but its attached EBS volumes bill for their full size every month.",
			Fix:           "Terminate the instance (snapshot first if needed), or restart it if it is still in use.",
			EstMonthlyUSD: est,
		})
	}
}

// checkSnapshotsAndImages flags forgotten snapshots and AMIs. A snapshot
// backing an AMI is attributed to the AMI finding only, never both.
func checkSnapshotsAndImages(snap CostSnapshot, out *[]Finding) {
	amiSnapshotIDs := make(map[string]bool)
	for _, img := range snap.Images {
		for _, sid := range img.SnapshotIDs {
			amiSnapshotIDs[sid] = true
		}
	}
	snapSizeByID := make(map[string]int32, len(snap.Snapshots))
	for _, s := range snap.Snapshots {
		snapSizeByID[s.ID] = s.SizeGiB
	}
	usedImages := make(map[string]bool)
	for _, in := range snap.Instances {
		if in.ImageID != "" {
			usedImages[in.ImageID] = true
		}
	}

	for _, s := range snap.Snapshots {
		if amiSnapshotIDs[s.ID] {
			continue
		}
		if s.Started.IsZero() || snap.Now.Sub(s.Started) < oldSnapshotAge {
			continue
		}
		ageDays := int(snap.Now.Sub(s.Started).Hours() / 24)
		*out = append(*out, Finding{
			ID:            CheckOldSnapshot,
			Severity:      SevInfo,
			Service:       "ec2",
			Region:        snap.Region,
			Resource:      s.ID,
			Title:         fmt.Sprintf("EBS snapshot is %d days old and not referenced by any AMI", ageDays),
			Detail:        "Old snapshots with no AMI referencing them are usually forgotten backups. Snapshots are incremental, so the estimate is an upper bound.",
			Fix:           "Delete the snapshot if its data is no longer needed, or move it to the cheaper archive tier.",
			EstMonthlyUSD: costs.SnapshotMonth(s.SizeGiB),
		})
	}

	if !snap.InstancesComplete {
		return // can't prove an AMI unused from a partial instance listing
	}
	for _, img := range snap.Images {
		if usedImages[img.ID] {
			continue
		}
		if img.Created.IsZero() || snap.Now.Sub(img.Created) < unusedImageAge {
			continue
		}
		var est float64
		for _, sid := range img.SnapshotIDs {
			est += costs.SnapshotMonth(snapSizeByID[sid])
		}
		ageDays := int(snap.Now.Sub(img.Created).Hours() / 24)
		res := img.ID
		if img.Name != "" {
			res = fmt.Sprintf("%s (%s)", img.ID, img.Name)
		}
		*out = append(*out, Finding{
			ID:            CheckUnusedAMI,
			Severity:      SevInfo,
			Service:       "ec2",
			Region:        snap.Region,
			Resource:      res,
			Title:         fmt.Sprintf("AMI is %d days old and no instance uses it", ageDays),
			Detail:        fmt.Sprintf("No instance in this region was launched from the image, and it is backed by %d snapshot(s) that keep billing.", len(img.SnapshotIDs)),
			Fix:           "Deregister the AMI and delete its backing snapshots if it is no longer needed.",
			EstMonthlyUSD: est,
		})
	}
}

// checkDynamoDBTables flags provisioned tables whose observed consumption is
// a small fraction of what is provisioned.
func checkDynamoDBTables(snap CostSnapshot, out *[]Finding) {
	for _, t := range snap.Tables {
		prov := t.ProvisionedRCU + t.ProvisionedWCU
		if prov < ddbMinProvisionedUnits {
			continue // on-demand, or too small to matter
		}
		if t.AvgConsumedRCU == nil || t.AvgConsumedWCU == nil {
			continue // metrics unavailable
		}
		if t.Created.IsZero() || snap.Now.Sub(t.Created) < lbIdleWindow {
			continue // too new for the lookback to mean anything
		}
		util := (*t.AvgConsumedRCU + *t.AvgConsumedWCU) / float64(prov)
		if util >= ddbLowUtilFrac {
			continue
		}
		provCost := costs.DynamoDBProvisionedMonth(float64(t.ProvisionedRCU), float64(t.ProvisionedWCU))
		neededCost := costs.DynamoDBProvisionedMonth(
			*t.AvgConsumedRCU*ddbHeadroomFactor,
			*t.AvgConsumedWCU*ddbHeadroomFactor,
		)
		est := provCost - neededCost
		if est <= 0 {
			continue
		}
		*out = append(*out, Finding{
			ID:       CheckDDBOverProvision,
			Severity: SevWarning,
			Service:  "dynamodb",
			Region:   snap.Region,
			Resource: t.Name,
			ARN:      t.ARN,
			Title:    fmt.Sprintf("Table uses %s of its provisioned capacity", percent(util)),
			Detail: fmt.Sprintf("Provisioned %d RCU / %d WCU, but 14-day average consumption is %.1f RCU / %.1f WCU.",
				t.ProvisionedRCU, t.ProvisionedWCU, *t.AvgConsumedRCU, *t.AvgConsumedWCU),
			Fix:           "Lower the provisioned capacity, enable auto scaling, or switch the table to on-demand billing.",
			EstMonthlyUSD: est,
		})
	}
}

// percent renders a fraction as a short percentage ("3%", "<1%").
func percent(frac float64) string {
	p := frac * 100
	if p > 0 && p < 1 {
		return "<1%"
	}
	return strings.TrimSuffix(fmt.Sprintf("%.0f", p), ".0") + "%"
}
