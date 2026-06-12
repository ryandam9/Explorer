package audit

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awselbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// errRecorder accumulates best-effort collection errors for one region.
type errRecorder struct {
	region string
	errs   []model.ExploreError
}

// record classifies and stores a collection error. service names the AWS
// service whose call failed, so the error summary reads like the scan
// engine's ("ec2@us-east-1: AccessDenied …").
func (r *errRecorder) record(service string, err error) {
	if err == nil {
		return
	}
	code := "CollectionError"
	msg := err.Error()
	if awserr.IsAuthError(err) {
		code = "AccessDenied"
		msg = awserr.FriendlyMessage(err, service)
	}
	r.errs = append(r.errs, model.ExploreError{
		Service: service, Region: r.region, Code: code, Message: msg,
	})
}

// withTimeout bounds a service-family collection; d <= 0 means no timeout.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// collectCostRegion gathers the cost snapshot for one region. Every service
// family is fetched independently: a failure empties that part of the
// snapshot (its checks then produce no findings) and is reported, while the
// rest of the audit proceeds.
func collectCostRegion(ctx context.Context, baseCfg aws.Config, region string, perCallTimeout time.Duration) (findings.CostSnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.CostSnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	collectEC2Cost(ctx, cfg, &snap, rec, perCallTimeout)
	collectELBCost(ctx, cfg, &snap, rec, perCallTimeout)
	collectDynamoDBCost(ctx, cfg, &snap, rec, perCallTimeout)
	fetchCostMetrics(ctx, cfg, &snap, rec, perCallTimeout)

	return snap, rec.errs
}

// ---------------------------------------------------------------------------
// EC2: volumes, addresses, NAT gateways, route tables, instances, snapshots,
// AMIs.
// ---------------------------------------------------------------------------

func collectEC2Cost(ctx context.Context, cfg aws.Config, snap *findings.CostSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsec2.NewFromConfig(cfg)

	volPager := awsec2.NewDescribeVolumesPaginator(client, &awsec2.DescribeVolumesInput{})
	for volPager.HasMorePages() {
		page, err := volPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, v := range page.Volumes {
			snap.Volumes = append(snap.Volumes, mapCostVolume(v))
		}
	}

	if out, err := client.DescribeAddresses(ctx, &awsec2.DescribeAddressesInput{}); err != nil {
		rec.record("ec2", err)
	} else {
		for _, a := range out.Addresses {
			snap.Addresses = append(snap.Addresses, mapCostAddress(a))
		}
	}

	natPager := awsec2.NewDescribeNatGatewaysPaginator(client, &awsec2.DescribeNatGatewaysInput{})
	for natPager.HasMorePages() {
		page, err := natPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, n := range page.NatGateways {
			snap.NatGateways = append(snap.NatGateways, mapCostNatGateway(n))
		}
	}

	snap.NatRouteRefs = map[string]bool{}
	rtPager := awsec2.NewDescribeRouteTablesPaginator(client, &awsec2.DescribeRouteTablesInput{})
	for rtPager.HasMorePages() {
		page, err := rtPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			// Without route data the idle-NAT check would flag every gateway;
			// dropping the gateways instead skips the check entirely.
			snap.NatGateways = nil
			break
		}
		for _, rt := range page.RouteTables {
			addNatRouteRefs(snap.NatRouteRefs, rt)
		}
	}

	instPager := awsec2.NewDescribeInstancesPaginator(client, &awsec2.DescribeInstancesInput{})
	snap.InstancesComplete = true
	for instPager.HasMorePages() {
		page, err := instPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			snap.InstancesComplete = false
			break
		}
		for _, res := range page.Reservations {
			for _, in := range res.Instances {
				snap.Instances = append(snap.Instances, mapCostInstance(in))
			}
		}
	}

	snapPager := awsec2.NewDescribeSnapshotsPaginator(client, &awsec2.DescribeSnapshotsInput{
		OwnerIds: []string{"self"},
	})
	for snapPager.HasMorePages() {
		page, err := snapPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, s := range page.Snapshots {
			snap.Snapshots = append(snap.Snapshots, mapCostSnapshot(s))
		}
	}

	imgPager := awsec2.NewDescribeImagesPaginator(client, &awsec2.DescribeImagesInput{
		Owners: []string{"self"},
	})
	for imgPager.HasMorePages() {
		page, err := imgPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			// Without AMI data the old-snapshot check can't tell which
			// snapshots back images; drop snapshots so it skips rather than
			// flagging AMI-backed ones.
			snap.Snapshots = nil
			break
		}
		for _, img := range page.Images {
			snap.Images = append(snap.Images, mapCostImage(img))
		}
	}
}

func mapCostVolume(v ec2types.Volume) findings.CostVolume {
	return findings.CostVolume{
		ID:       aws.ToString(v.VolumeId),
		Type:     string(v.VolumeType),
		SizeGiB:  aws.ToInt32(v.Size),
		State:    string(v.State),
		Attached: len(v.Attachments) > 0,
	}
}

func mapCostAddress(a ec2types.Address) findings.CostAddress {
	return findings.CostAddress{
		ID:         aws.ToString(a.AllocationId),
		PublicIP:   aws.ToString(a.PublicIp),
		Associated: a.AssociationId != nil,
	}
}

func mapCostNatGateway(n ec2types.NatGateway) findings.CostNatGateway {
	return findings.CostNatGateway{
		ID:    aws.ToString(n.NatGatewayId),
		Name:  nameTag(n.Tags),
		State: string(n.State),
	}
}

// addNatRouteRefs records every NAT gateway a route table routes through.
func addNatRouteRefs(refs map[string]bool, rt ec2types.RouteTable) {
	for _, r := range rt.Routes {
		if id := aws.ToString(r.NatGatewayId); id != "" {
			refs[id] = true
		}
	}
}

func mapCostInstance(in ec2types.Instance) findings.CostInstance {
	ci := findings.CostInstance{
		ID:      aws.ToString(in.InstanceId),
		Name:    nameTag(in.Tags),
		ImageID: aws.ToString(in.ImageId),
	}
	if in.State != nil {
		ci.State = string(in.State.Name)
	}
	for _, bdm := range in.BlockDeviceMappings {
		if bdm.Ebs != nil && bdm.Ebs.VolumeId != nil {
			ci.VolumeIDs = append(ci.VolumeIDs, *bdm.Ebs.VolumeId)
		}
	}
	return ci
}

func mapCostSnapshot(s ec2types.Snapshot) findings.CostEBSSnapshot {
	return findings.CostEBSSnapshot{
		ID:          aws.ToString(s.SnapshotId),
		SizeGiB:     aws.ToInt32(s.VolumeSize),
		Started:     aws.ToTime(s.StartTime),
		Description: aws.ToString(s.Description),
	}
}

func mapCostImage(img ec2types.Image) findings.CostImage {
	ci := findings.CostImage{
		ID:   aws.ToString(img.ImageId),
		Name: aws.ToString(img.Name),
	}
	// CreationDate is an RFC3339 string; an unparsable value leaves Created
	// zero, which exempts the image from the age check.
	if t, err := time.Parse(time.RFC3339, aws.ToString(img.CreationDate)); err == nil {
		ci.Created = t
	}
	for _, bdm := range img.BlockDeviceMappings {
		if bdm.Ebs != nil && bdm.Ebs.SnapshotId != nil {
			ci.SnapshotIDs = append(ci.SnapshotIDs, *bdm.Ebs.SnapshotId)
		}
	}
	return ci
}

func nameTag(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// ELBv2: load balancers, target groups, target health.
// ---------------------------------------------------------------------------

func collectELBCost(ctx context.Context, cfg aws.Config, snap *findings.CostSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awselbv2.NewFromConfig(cfg)

	// Accumulate behind pointers and flush into the snapshot at the end, so
	// target-health mutations below can't be lost to slice reallocation.
	var lbs []*findings.CostLoadBalancer
	byARN := map[string]*findings.CostLoadBalancer{}
	defer func() {
		for _, lb := range lbs {
			snap.LoadBalancers = append(snap.LoadBalancers, *lb)
		}
	}()

	lbPager := awselbv2.NewDescribeLoadBalancersPaginator(client, &awselbv2.DescribeLoadBalancersInput{})
	for lbPager.HasMorePages() {
		page, err := lbPager.NextPage(ctx)
		if err != nil {
			rec.record("elbv2", err)
			return
		}
		for _, lb := range page.LoadBalancers {
			clb := mapCostLoadBalancer(lb)
			lbs = append(lbs, &clb)
			byARN[clb.ARN] = &clb
		}
	}
	if len(byARN) == 0 {
		return
	}

	// One listing of all target groups (each names the LBs it serves) beats a
	// DescribeTargetGroups call per load balancer.
	type tgRef struct {
		arn    string
		lbARNs []string
	}
	var tgs []tgRef
	tgPager := awselbv2.NewDescribeTargetGroupsPaginator(client, &awselbv2.DescribeTargetGroupsInput{})
	for tgPager.HasMorePages() {
		page, err := tgPager.NextPage(ctx)
		if err != nil {
			rec.record("elbv2", err)
			return // health stays unknown for every LB; the check skips
		}
		for _, tg := range page.TargetGroups {
			tgs = append(tgs, tgRef{arn: aws.ToString(tg.TargetGroupArn), lbARNs: tg.LoadBalancerArns})
		}
	}

	for _, lb := range byARN {
		lb.HealthKnown = true
	}
	healthErrReported := false
	for _, tg := range tgs {
		if len(tg.lbARNs) == 0 {
			continue // target group not attached to any load balancer
		}
		out, err := client.DescribeTargetHealth(ctx, &awselbv2.DescribeTargetHealthInput{
			TargetGroupArn: aws.String(tg.arn),
		})
		if err != nil {
			if !healthErrReported {
				rec.record("elbv2", err)
				healthErrReported = true
			}
			for _, lbARN := range tg.lbARNs {
				if lb := byARN[lbARN]; lb != nil {
					lb.HealthKnown = false
				}
			}
			continue
		}
		total, healthy := countTargetHealth(out.TargetHealthDescriptions)
		for _, lbARN := range tg.lbARNs {
			if lb := byARN[lbARN]; lb != nil {
				lb.TargetGroups++
				lb.TotalTargets += total
				lb.HealthyTargets += healthy
			}
		}
	}
}

func mapCostLoadBalancer(lb elbv2types.LoadBalancer) findings.CostLoadBalancer {
	return findings.CostLoadBalancer{
		ARN:     aws.ToString(lb.LoadBalancerArn),
		Name:    aws.ToString(lb.LoadBalancerName),
		Type:    string(lb.Type),
		Created: aws.ToTime(lb.CreatedTime),
	}
}

func countTargetHealth(ths []elbv2types.TargetHealthDescription) (total, healthy int) {
	for _, th := range ths {
		total++
		if th.TargetHealth != nil && th.TargetHealth.State == elbv2types.TargetHealthStateEnumHealthy {
			healthy++
		}
	}
	return
}

// ---------------------------------------------------------------------------
// DynamoDB: tables with provisioned capacity.
// ---------------------------------------------------------------------------

func collectDynamoDBCost(ctx context.Context, cfg aws.Config, snap *findings.CostSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsdynamodb.NewFromConfig(cfg)

	var names []string
	pager := awsdynamodb.NewListTablesPaginator(client, &awsdynamodb.ListTablesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("dynamodb", err)
			return
		}
		names = append(names, page.TableNames...)
	}

	describeErrReported := false
	for _, name := range names {
		out, err := client.DescribeTable(ctx, &awsdynamodb.DescribeTableInput{TableName: aws.String(name)})
		if err != nil {
			if !describeErrReported {
				rec.record("dynamodb", err)
				describeErrReported = true
			}
			continue
		}
		if out.Table == nil {
			continue
		}
		t := findings.CostTable{
			Name:    name,
			ARN:     aws.ToString(out.Table.TableArn),
			Created: aws.ToTime(out.Table.CreationDateTime),
		}
		if pt := out.Table.ProvisionedThroughput; pt != nil {
			t.ProvisionedRCU = aws.ToInt64(pt.ReadCapacityUnits)
			t.ProvisionedWCU = aws.ToInt64(pt.WriteCapacityUnits)
		}
		snap.Tables = append(snap.Tables, t)
	}
}
