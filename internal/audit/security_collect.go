package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudwatch "github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Per-resource describe calls (bucket posture, snapshot attributes, function
// URLs, queue/topic policies) are capped so a huge account audits in bounded
// time; hitting a cap is reported as a Truncated note, never silently.
const (
	maxBucketChecks      = 200
	maxRDSSnapshotChecks = 100
	maxFunctionURLChecks = 200
	maxQueuePolicyChecks = 200
	maxTopicPolicyChecks = 200
)

// recordTruncation notes that a per-resource sweep stopped at its cap, so
// "no findings" past the cap reads as "not checked", not "clean".
func (r *errRecorder) recordTruncation(service, what string, cap int) {
	r.errs = append(r.errs, model.ExploreError{
		Service: service, Region: r.region, Code: "Truncated",
		Message: fmt.Sprintf("security audit checked the first %d %s only", cap, what),
	})
}

// collectSecurityRegion gathers the security snapshot for one region.
// includeS3 marks the single designated region that also sweeps the
// account-global S3 namespace. Same best-effort contract as the cost
// collector: a failed family empties its part of the snapshot and is
// reported, never fatal.
func collectSecurityRegion(ctx context.Context, baseCfg aws.Config, region string, includeS3 bool, perCallTimeout time.Duration) (findings.SecuritySnapshot, []model.ExploreError) {
	cfg := baseCfg
	cfg.Region = region

	snap := findings.SecuritySnapshot{Region: region, Now: time.Now().UTC()}
	rec := &errRecorder{region: region}

	collectEC2Security(ctx, cfg, &snap, rec, perCallTimeout)
	collectRDSSecurity(ctx, cfg, &snap, rec, perCallTimeout)
	collectLambdaSecurity(ctx, cfg, &snap, rec, perCallTimeout)
	collectSQSSecurity(ctx, cfg, &snap, rec, perCallTimeout)
	collectSNSSecurity(ctx, cfg, &snap, rec, perCallTimeout)
	collectAlarmSecurity(ctx, cfg, &snap, rec, perCallTimeout)
	if includeS3 {
		collectS3Security(ctx, cfg, &snap, rec, perCallTimeout)
	}

	return snap, rec.errs
}

func collectEC2Security(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
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
			snap.Volumes = append(snap.Volumes, findings.SecVolume{
				ID:        aws.ToString(v.VolumeId),
				Encrypted: aws.ToBool(v.Encrypted),
			})
		}
	}

	if out, err := client.GetEbsEncryptionByDefault(ctx, &awsec2.GetEbsEncryptionByDefaultInput{}); err != nil {
		rec.record("ec2", err)
	} else {
		snap.EBSDefaultEncryption = out.EbsEncryptionByDefault
	}

	// One call finds every publicly restorable self-owned snapshot — no
	// per-snapshot DescribeSnapshotAttribute sweep needed.
	pubPager := awsec2.NewDescribeSnapshotsPaginator(client, &awsec2.DescribeSnapshotsInput{
		OwnerIds:            []string{"self"},
		RestorableByUserIds: []string{"all"},
	})
	for pubPager.HasMorePages() {
		page, err := pubPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, s := range page.Snapshots {
			snap.PublicEBSSnapshots = append(snap.PublicEBSSnapshots, aws.ToString(s.SnapshotId))
		}
	}

	instPager := awsec2.NewDescribeInstancesPaginator(client, &awsec2.DescribeInstancesInput{})
	for instPager.HasMorePages() {
		page, err := instPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, res := range page.Reservations {
			for _, i := range res.Instances {
				si := findings.SecInstance{ID: aws.ToString(i.InstanceId)}
				if i.State != nil {
					si.State = string(i.State.Name)
				}
				if mo := i.MetadataOptions; mo != nil {
					si.HTTPTokens = string(mo.HttpTokens)
					si.HTTPEndpoint = string(mo.HttpEndpoint)
				}
				for _, t := range i.Tags {
					if aws.ToString(t.Key) == "Name" {
						si.Name = aws.ToString(t.Value)
					}
				}
				snap.Instances = append(snap.Instances, si)
			}
		}
	}

	sgPager := awsec2.NewDescribeSecurityGroupsPaginator(client, &awsec2.DescribeSecurityGroupsInput{})
	for sgPager.HasMorePages() {
		page, err := sgPager.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, sg := range page.SecurityGroups {
			g := findings.SecGroup{
				ID:   aws.ToString(sg.GroupId),
				Name: aws.ToString(sg.GroupName),
			}
			for _, p := range sg.IpPermissions {
				for _, src := range worldOpenSources(p) {
					g.Rules = append(g.Rules, findings.SecSGRule{
						Protocol: aws.ToString(p.IpProtocol),
						FromPort: orAll(p.FromPort),
						ToPort:   orAll(p.ToPort),
						Source:   src,
					})
				}
			}
			snap.SecurityGroups = append(snap.SecurityGroups, g)
		}
	}
}

// worldOpenSources returns the world-open CIDR sources of an inbound rule.
func worldOpenSources(p ec2types.IpPermission) []string {
	var out []string
	for _, r := range p.IpRanges {
		if aws.ToString(r.CidrIp) == "0.0.0.0/0" {
			out = append(out, "0.0.0.0/0")
		}
	}
	for _, r := range p.Ipv6Ranges {
		if aws.ToString(r.CidrIpv6) == "::/0" {
			out = append(out, "::/0")
		}
	}
	return out
}

// orAll maps a nil port (protocol "-1" rules carry none) to -1 = all ports.
func orAll(p *int32) int32 {
	if p == nil {
		return -1
	}
	return *p
}

func collectRDSSecurity(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awsrds.NewFromConfig(cfg)

	dbPager := awsrds.NewDescribeDBInstancesPaginator(client, &awsrds.DescribeDBInstancesInput{})
	for dbPager.HasMorePages() {
		page, err := dbPager.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, db := range page.DBInstances {
			snap.DBInstances = append(snap.DBInstances, findings.SecDBInstance{
				ID:               aws.ToString(db.DBInstanceIdentifier),
				PublicAccessible: aws.ToBool(db.PubliclyAccessible),
				StorageEncrypted: aws.ToBool(db.StorageEncrypted),
			})
		}
	}

	// Public sharing needs one attribute call per snapshot; cap the sweep.
	var snapIDs []string
	snapPager := awsrds.NewDescribeDBSnapshotsPaginator(client, &awsrds.DescribeDBSnapshotsInput{
		SnapshotType: aws.String("manual"), // only manual snapshots can be shared
	})
	for snapPager.HasMorePages() {
		page, err := snapPager.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, s := range page.DBSnapshots {
			snapIDs = append(snapIDs, aws.ToString(s.DBSnapshotIdentifier))
		}
		if len(snapIDs) >= maxRDSSnapshotChecks {
			rec.recordTruncation("rds", "manual DB snapshots", maxRDSSnapshotChecks)
			snapIDs = snapIDs[:maxRDSSnapshotChecks]
			break
		}
	}
	for _, id := range snapIDs {
		out, err := client.DescribeDBSnapshotAttributes(ctx, &awsrds.DescribeDBSnapshotAttributesInput{
			DBSnapshotIdentifier: aws.String(id),
		})
		if err != nil {
			rec.record("rds", err)
			break
		}
		if out.DBSnapshotAttributesResult == nil {
			continue
		}
		for _, attr := range out.DBSnapshotAttributesResult.DBSnapshotAttributes {
			if aws.ToString(attr.AttributeName) != "restore" {
				continue
			}
			for _, v := range attr.AttributeValues {
				if v == "all" {
					snap.PublicRDSSnapshots = append(snap.PublicRDSSnapshots, id)
				}
			}
		}
	}
}

func collectLambdaSecurity(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awslambda.NewFromConfig(cfg)

	var names []string
	pager := awslambda.NewListFunctionsPaginator(client, &awslambda.ListFunctionsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("lambda", err)
			return
		}
		for _, fn := range page.Functions {
			names = append(names, aws.ToString(fn.FunctionName))
		}
		if len(names) >= maxFunctionURLChecks {
			rec.recordTruncation("lambda", "functions", maxFunctionURLChecks)
			names = names[:maxFunctionURLChecks]
			break
		}
	}

	for _, name := range names {
		fn := findings.SecFunction{Name: name}
		urls, err := client.ListFunctionUrlConfigs(ctx, &awslambda.ListFunctionUrlConfigsInput{
			FunctionName: aws.String(name),
		})
		if err != nil {
			rec.record("lambda", err)
			snap.Functions = append(snap.Functions, fn)
			break
		}
		for _, u := range urls.FunctionUrlConfigs {
			if u.AuthType == lambdatypes.FunctionUrlAuthTypeNone {
				fn.URLNoAuth = true
			}
		}
		snap.Functions = append(snap.Functions, fn)
	}
}

func collectSQSSecurity(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awssqs.NewFromConfig(cfg)

	var urls []string
	pager := awssqs.NewListQueuesPaginator(client, &awssqs.ListQueuesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("sqs", err)
			return
		}
		urls = append(urls, page.QueueUrls...)
		if len(urls) >= maxQueuePolicyChecks {
			rec.recordTruncation("sqs", "queues", maxQueuePolicyChecks)
			urls = urls[:maxQueuePolicyChecks]
			break
		}
	}

	for _, u := range urls {
		out, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
			QueueUrl:       aws.String(u),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNamePolicy},
		})
		if err != nil {
			rec.record("sqs", err)
			break
		}
		name := u
		if i := strings.LastIndexByte(u, '/'); i >= 0 {
			name = u[i+1:]
		}
		snap.Queues = append(snap.Queues, findings.SecQueue{
			Name:   name,
			Policy: out.Attributes[string(sqstypes.QueueAttributeNamePolicy)],
		})
	}
}

func collectSNSSecurity(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awssns.NewFromConfig(cfg)

	var arns []string
	pager := awssns.NewListTopicsPaginator(client, &awssns.ListTopicsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("sns", err)
			return
		}
		for _, t := range page.Topics {
			arns = append(arns, aws.ToString(t.TopicArn))
		}
		if len(arns) >= maxTopicPolicyChecks {
			rec.recordTruncation("sns", "topics", maxTopicPolicyChecks)
			arns = arns[:maxTopicPolicyChecks]
			break
		}
	}

	for _, arn := range arns {
		out, err := client.GetTopicAttributes(ctx, &awssns.GetTopicAttributesInput{TopicArn: aws.String(arn)})
		if err != nil {
			rec.record("sns", err)
			break
		}
		name := arn
		if i := strings.LastIndexByte(arn, ':'); i >= 0 {
			name = arn[i+1:]
		}
		snap.Topics = append(snap.Topics, findings.SecTopic{
			ARN: arn, Name: name, Policy: out.Attributes["Policy"],
		})
	}
}

func collectAlarmSecurity(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	client := awscloudwatch.NewFromConfig(cfg)

	pager := awscloudwatch.NewDescribeAlarmsPaginator(client, &awscloudwatch.DescribeAlarmsInput{
		StateValue: cwtypes.StateValueInsufficientData,
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("cloudwatch", err)
			break
		}
		for _, a := range page.MetricAlarms {
			snap.Alarms = append(snap.Alarms, findings.SecAlarm{
				Name:         aws.ToString(a.AlarmName),
				StateUpdated: aws.ToTime(a.StateUpdatedTimestamp),
			})
		}
	}
}

// collectS3Security sweeps the account-global bucket namespace: posture
// calls must go to each bucket's own region, so clients are created per
// bucket region as needed.
func collectS3Security(ctx context.Context, cfg aws.Config, snap *findings.SecuritySnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	snap.S3Scanned = true

	base := awss3.NewFromConfig(cfg)
	out, err := base.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		rec.record("s3", err)
		return
	}
	buckets := out.Buckets
	if len(buckets) > maxBucketChecks {
		rec.recordTruncation("s3", "buckets", maxBucketChecks)
		buckets = buckets[:maxBucketChecks]
	}

	clients := map[string]*awss3.Client{cfg.Region: base}
	clientFor := func(region string) *awss3.Client {
		if c, ok := clients[region]; ok {
			return c
		}
		rcfg := cfg
		rcfg.Region = region
		c := awss3.NewFromConfig(rcfg)
		clients[region] = c
		return c
	}

	for _, b := range buckets {
		name := aws.ToString(b.Name)
		sb := findings.SecBucket{Name: name}

		region := cfg.Region
		if loc, err := base.GetBucketLocation(ctx, &awss3.GetBucketLocationInput{Bucket: b.Name}); err == nil {
			region = string(loc.LocationConstraint)
			if region == "" {
				region = "us-east-1" // the API's legacy null for us-east-1
			}
		} else {
			rec.record("s3", err)
		}
		sb.Region = region
		client := clientFor(region)

		if ps, err := client.GetBucketPolicyStatus(ctx, &awss3.GetBucketPolicyStatusInput{Bucket: b.Name}); err == nil {
			if ps.PolicyStatus != nil {
				sb.PolicyPublic = ps.PolicyStatus.IsPublic
			}
		} else if isS3NotFound(err) {
			f := false
			sb.PolicyPublic = &f // no policy at all cannot be public
		}

		if pab, err := client.GetPublicAccessBlock(ctx, &awss3.GetPublicAccessBlockInput{Bucket: b.Name}); err == nil {
			if c := pab.PublicAccessBlockConfiguration; c != nil {
				on := aws.ToBool(c.BlockPublicAcls) && aws.ToBool(c.BlockPublicPolicy) &&
					aws.ToBool(c.IgnorePublicAcls) && aws.ToBool(c.RestrictPublicBuckets)
				sb.PABAllOn = &on
			}
		} else if isS3NotFound(err) {
			f := false
			sb.PABAllOn = &f // no configuration = nothing blocked
		}

		if _, err := client.GetBucketEncryption(ctx, &awss3.GetBucketEncryptionInput{Bucket: b.Name}); err == nil {
			tr := true
			sb.Encrypted = &tr
		} else if isS3NotFound(err) {
			f := false
			sb.Encrypted = &f
		}

		snap.Buckets = append(snap.Buckets, sb)
	}
}

// isS3NotFound matches the "no such configuration" error family, which for
// these posture calls is a definitive negative answer, not a failure.
func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{
		"NoSuchBucketPolicy", "NoSuchPublicAccessBlockConfiguration",
		"ServerSideEncryptionConfigurationNotFoundError", "NotFound",
	} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}
