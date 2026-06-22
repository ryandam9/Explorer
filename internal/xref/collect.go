package xref

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	awselbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	awssecrets "github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"golang.org/x/sync/errgroup"

	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Collection is best-effort in the established pattern: each source is fetched
// independently, a failure empties that source (its edges are simply absent,
// reported as an error) and never aborts the scan. Because a missing source
// silently narrows what "not referenced" means, the error list should always
// be surfaced to the user alongside the result.

type recorder struct {
	region string
	errs   []model.ExploreError
}

func (r *recorder) record(service string, err error) {
	if err == nil {
		return
	}
	code, msg := awserr.Classify(err, service, "")
	r.errs = append(r.errs, model.ExploreError{Service: service, Region: r.region, Code: code, Message: msg})
}

func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// Collect gathers reference edges across the given regions (plus the global
// IAM roles and instance profiles) and returns them with any collection
// errors. The result feeds BuildIndex.
func Collect(ctx context.Context, baseCfg aws.Config, regions []string, maxConcurrency int, perCallTimeout time.Duration) ([]Edge, []model.ExploreError) {
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	if len(regions) == 0 {
		regions = []string{"us-east-1"}
	}

	// IAM is global; resolve instance-profile → role mappings and trust-policy
	// edges once, up front, so per-region EC2 collection can attribute instance
	// profiles to their roles.
	gctx, gcancel := withTimeout(ctx, perCallTimeout)
	profiles, trustEdges, iamErrs := collectIAM(gctx, baseCfg)
	gcancel()

	type result struct {
		edges []Edge
		errs  []model.ExploreError
	}
	results := make([]result, len(regions))

	g, ggctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for i, region := range regions {
		i, region := i, region
		g.Go(func() error {
			edges, errs := collectRegion(ggctx, baseCfg, region, profiles, perCallTimeout)
			results[i] = result{edges: edges, errs: errs}
			return nil
		})
	}
	_ = g.Wait()

	edges := append([]Edge(nil), trustEdges...)
	errs := append([]model.ExploreError(nil), iamErrs...)
	for _, r := range results {
		edges = append(edges, r.edges...)
		errs = append(errs, r.errs...)
	}

	// S3 is listed globally but called per-bucket-region, so it runs once here
	// rather than in the per-region fan-out (§3).
	s3Edges, s3Errs := collectS3(ctx, baseCfg, regions, maxConcurrency, perCallTimeout)
	edges = append(edges, s3Edges...)
	errs = append(errs, s3Errs...)

	// CloudFront and Route 53 are global; collect them once (§3).
	netEdges, netErrs := collectGlobalNetworking(ctx, baseCfg, perCallTimeout)
	edges = append(edges, netEdges...)
	errs = append(errs, netErrs...)

	return edges, errs
}

// --- Global IAM ---------------------------------------------------------------

// collectIAM returns instance-profile → role ARNs and the trust-policy edges
// (roles whose AssumeRole policy names another principal).
func collectIAM(ctx context.Context, baseCfg aws.Config) (map[string][]string, []Edge, []model.ExploreError) {
	rec := &recorder{region: "global"}
	client := awsiam.NewFromConfig(baseCfg)
	profiles := map[string][]string{}

	pp := awsiam.NewListInstanceProfilesPaginator(client, &awsiam.ListInstanceProfilesInput{})
	for pp.HasMorePages() {
		page, err := pp.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			break
		}
		for _, p := range page.InstanceProfiles {
			arn := aws.ToString(p.Arn)
			for _, role := range p.Roles {
				profiles[arn] = append(profiles[arn], aws.ToString(role.Arn))
			}
		}
	}

	var edges []Edge
	rp := awsiam.NewListRolesPaginator(client, &awsiam.ListRolesInput{})
	for rp.HasMorePages() {
		page, err := rp.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			break
		}
		for _, role := range page.Roles {
			roleRef := Reference{Service: "iam", Type: "role", Region: "global",
				ID: aws.ToString(role.Arn), Name: aws.ToString(role.RoleName)}
			for _, principal := range trustPrincipals(aws.ToString(role.AssumeRolePolicyDocument)) {
				edges = append(edges, Edge{From: withVia(roleRef, "trust policy principal"), Target: principal})
			}
			edges = append(edges, rolePolicyEdges(ctx, client, roleRef, rec)...)
		}
	}
	return profiles, edges, rec.errs
}

// trustPrincipals extracts the AWS principal ARNs from a URL-encoded IAM trust
// policy document. Non-ARN principals (services, "*") are ignored.
func trustPrincipals(doc string) []string {
	if doc == "" {
		return nil
	}
	if dec, err := url.QueryUnescape(doc); err == nil {
		doc = dec
	}
	var parsed struct {
		Statement []struct {
			Principal struct {
				AWS json.RawMessage `json:"AWS"`
			} `json:"Principal"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		return nil
	}
	var out []string
	for _, st := range parsed.Statement {
		for _, p := range rawStrings(st.Principal.AWS) {
			if strings.HasPrefix(p, "arn:") {
				out = append(out, p)
			}
		}
	}
	return out
}

// rawStrings decodes a JSON value that may be a string or an array of strings.
func rawStrings(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	return nil
}

// --- Per-region ---------------------------------------------------------------

func collectRegion(ctx context.Context, baseCfg aws.Config, region string, profiles map[string][]string, timeout time.Duration) ([]Edge, []model.ExploreError) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	cfg := baseCfg
	cfg.Region = region
	rec := &recorder{region: region}

	var edges []Edge
	edges = append(edges, lambdaEdges(ctx, cfg, region, rec)...)
	edges = append(edges, ec2Edges(ctx, cfg, region, profiles, rec)...)
	edges = append(edges, rdsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, secretsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, sqsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, ecsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, eksEdges(ctx, cfg, region, rec)...)
	edges = append(edges, elbv2Edges(ctx, cfg, region, rec)...)
	edges = append(edges, efsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, snsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, eventBridgeEdges(ctx, cfg, region, rec)...)
	edges = append(edges, sfnEdges(ctx, cfg, region, rec)...)
	edges = append(edges, kinesisEdges(ctx, cfg, region, rec)...)
	edges = append(edges, apiGatewayEdges(ctx, cfg, region, rec)...)
	edges = append(edges, vpcEndpointsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, kmsEdges(ctx, cfg, region, rec)...)
	edges = append(edges, dynamodbEdges(ctx, cfg, region, rec)...)
	edges = append(edges, elastiCacheEdges(ctx, cfg, region, rec)...)
	edges = append(edges, redshiftEdges(ctx, cfg, region, rec)...)
	return edges, rec.errs
}

func lambdaEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awslambda.NewFromConfig(cfg)
	var edges []Edge
	p := awslambda.NewListFunctionsPaginator(client, &awslambda.ListFunctionsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("lambda", err)
			break
		}
		for _, fn := range page.Functions {
			edges = append(edges, lambdaFunctionEdges(fn, region)...)
		}
	}
	edges = append(edges, listEventSourceEdges(ctx, client, region, rec)...)
	return edges
}

func ec2Edges(ctx context.Context, cfg aws.Config, region string, profiles map[string][]string, rec *recorder) []Edge {
	client := awsec2.NewFromConfig(cfg)
	var edges []Edge

	ip := awsec2.NewDescribeInstancesPaginator(client, &awsec2.DescribeInstancesInput{})
	for ip.HasMorePages() {
		page, err := ip.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				edges = append(edges, ec2InstanceEdges(inst, region, profiles)...)
			}
		}
	}

	edges = append(edges, collectEIPEdges(ctx, client, region, rec)...)

	vp := awsec2.NewDescribeVolumesPaginator(client, &awsec2.DescribeVolumesInput{})
	for vp.HasMorePages() {
		page, err := vp.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		edges = append(edges, ebsVolumeEdges(page.Volumes, region)...)
	}

	np := awsec2.NewDescribeNetworkInterfacesPaginator(client, &awsec2.DescribeNetworkInterfacesInput{})
	for np.HasMorePages() {
		page, err := np.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, eni := range page.NetworkInterfaces {
			from := Reference{Service: "ec2", Type: "network-interface", Region: region,
				ID: aws.ToString(eni.NetworkInterfaceId), Name: eniName(eni.Description, eni.Attachment)}
			for _, gid := range eni.Groups {
				if g := aws.ToString(gid.GroupId); g != "" {
					edges = append(edges, Edge{From: withVia(from, "attached security group"), Target: g})
				}
			}
		}
	}
	return edges
}

// ebsVolumeEdges maps EBS volumes to their encryption key and the instance each
// is attached to. Pure over the SDK page so it is unit-testable.
func ebsVolumeEdges(vols []ec2types.Volume, region string) []Edge {
	var edges []Edge
	for _, v := range vols {
		from := Reference{Service: "ec2", Type: "volume", Region: region, ID: aws.ToString(v.VolumeId)}
		if key := aws.ToString(v.KmsKeyId); key != "" {
			edges = append(edges, Edge{From: withVia(from, "volume encryption key"), Target: key})
		}
		for _, att := range v.Attachments {
			if inst := aws.ToString(att.InstanceId); inst != "" {
				edges = append(edges, Edge{From: withVia(from, "attached to instance"), Target: inst})
			}
		}
	}
	return edges
}

func secretsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awssecrets.NewFromConfig(cfg)
	var edges []Edge
	p := awssecrets.NewListSecretsPaginator(client, &awssecrets.ListSecretsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("secretsmanager", err)
			break
		}
		for _, s := range page.SecretList {
			from := Reference{Service: "secretsmanager", Type: "secret", Region: region,
				ID: aws.ToString(s.ARN), Name: aws.ToString(s.Name)}
			if key := aws.ToString(s.KmsKeyId); key != "" {
				edges = append(edges, Edge{From: withVia(from, "secret encryption key"), Target: key})
			}
			if lam := aws.ToString(s.RotationLambdaARN); lam != "" {
				edges = append(edges, Edge{From: withVia(from, "rotation Lambda"), Target: lam})
			}
		}
	}
	return edges
}

func sqsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awssqs.NewFromConfig(cfg)
	var edges []Edge
	p := awssqs.NewListQueuesPaginator(client, &awssqs.ListQueuesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("sqs", err)
			break
		}
		for _, qurl := range page.QueueUrls {
			attrs, err := client.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
				QueueUrl: aws.String(qurl),
				AttributeNames: []sqstypes.QueueAttributeName{
					sqstypes.QueueAttributeNameKmsMasterKeyId,
					sqstypes.QueueAttributeNameQueueArn,
					sqstypes.QueueAttributeNameRedrivePolicy,
				},
			})
			if err != nil {
				rec.record("sqs", err)
				continue
			}
			arn := attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
			from := Reference{Service: "sqs", Type: "queue", Region: region,
				ID: orDefault(arn, qurl), Name: queueName(qurl)}
			if key := attrs.Attributes[string(sqstypes.QueueAttributeNameKmsMasterKeyId)]; key != "" {
				edges = append(edges, Edge{From: withVia(from, "queue encryption key"), Target: key})
			}
			if dlq := sqsRedriveTarget(attrs.Attributes[string(sqstypes.QueueAttributeNameRedrivePolicy)]); dlq != "" {
				edges = append(edges, Edge{From: withVia(from, "dead-letter queue"), Target: dlq})
			}
		}
	}
	return edges
}

func ecsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsecs.NewFromConfig(cfg)
	var edges []Edge
	p := awsecs.NewListTaskDefinitionsPaginator(client, &awsecs.ListTaskDefinitionsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("ecs", err)
			break
		}
		for _, arn := range page.TaskDefinitionArns {
			out, err := client.DescribeTaskDefinition(ctx, &awsecs.DescribeTaskDefinitionInput{TaskDefinition: aws.String(arn)})
			if err != nil {
				rec.record("ecs", err)
				continue
			}
			td := out.TaskDefinition
			if td == nil {
				continue
			}
			edges = append(edges, ecsTaskDefEdges(*td, region)...)
		}
	}
	return edges
}

func eksEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awseks.NewFromConfig(cfg)
	var edges []Edge
	cp := awseks.NewListClustersPaginator(client, &awseks.ListClustersInput{})
	for cp.HasMorePages() {
		page, err := cp.NextPage(ctx)
		if err != nil {
			rec.record("eks", err)
			return edges
		}
		for _, name := range page.Clusters {
			cl, err := client.DescribeCluster(ctx, &awseks.DescribeClusterInput{Name: aws.String(name)})
			if err != nil {
				rec.record("eks", err)
				continue
			}
			if cl.Cluster != nil {
				edges = append(edges, eksClusterEdges(*cl.Cluster, region)...)
			}
			ngp := awseks.NewListNodegroupsPaginator(client, &awseks.ListNodegroupsInput{ClusterName: aws.String(name)})
			for ngp.HasMorePages() {
				ngPage, err := ngp.NextPage(ctx)
				if err != nil {
					rec.record("eks", err)
					break
				}
				for _, ng := range ngPage.Nodegroups {
					out, err := client.DescribeNodegroup(ctx, &awseks.DescribeNodegroupInput{
						ClusterName: aws.String(name), NodegroupName: aws.String(ng)})
					if err != nil {
						rec.record("eks", err)
						continue
					}
					if out.Nodegroup != nil {
						from := Reference{Service: "eks", Type: "nodegroup", Region: region,
							ID: aws.ToString(out.Nodegroup.NodegroupArn), Name: name + "/" + ng}
						if role := aws.ToString(out.Nodegroup.NodeRole); role != "" {
							edges = append(edges, Edge{From: withVia(from, "EKS node-group role"), Target: role})
						}
					}
				}
			}
		}
	}
	return edges
}

func elbv2Edges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awselbv2.NewFromConfig(cfg)
	var edges []Edge
	lbp := awselbv2.NewDescribeLoadBalancersPaginator(client, &awselbv2.DescribeLoadBalancersInput{})
	for lbp.HasMorePages() {
		page, err := lbp.NextPage(ctx)
		if err != nil {
			rec.record("elbv2", err)
			break
		}
		for _, lb := range page.LoadBalancers {
			edges = append(edges, elbLoadBalancerEdges(lb, region)...)
			lsp := awselbv2.NewDescribeListenersPaginator(client, &awselbv2.DescribeListenersInput{
				LoadBalancerArn: lb.LoadBalancerArn})
			for lsp.HasMorePages() {
				lsPage, err := lsp.NextPage(ctx)
				if err != nil {
					rec.record("elbv2", err)
					break
				}
				for _, ls := range lsPage.Listeners {
					from := Reference{Service: "elbv2", Type: "listener", Region: region,
						ID: aws.ToString(ls.ListenerArn), Name: aws.ToString(lb.LoadBalancerName)}
					for _, c := range ls.Certificates {
						if arn := aws.ToString(c.CertificateArn); arn != "" {
							edges = append(edges, Edge{From: withVia(from, "ELBv2 listener certificate"), Target: arn})
						}
					}
				}
			}
		}
	}
	edges = append(edges, elbTargetGroupEdges(ctx, client, region, rec)...)
	return edges
}

// --- helpers ------------------------------------------------------------------

func withVia(r Reference, via string) Reference {
	r.Via = via
	return r
}

// nameTag returns the value of the "Name" tag, "" if absent.
func nameTag(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

// eniName builds a readable label for an ENI from its description and (when
// attached) the instance it serves.
func eniName(description *string, att *ec2types.NetworkInterfaceAttachment) string {
	d := aws.ToString(description)
	if att != nil {
		if id := aws.ToString(att.InstanceId); id != "" {
			if d != "" {
				return d + " (" + id + ")"
			}
			return id
		}
	}
	return d
}

// queueName returns the trailing path segment of an SQS queue URL.
func queueName(qurl string) string {
	if i := strings.LastIndexByte(qurl, '/'); i >= 0 && i+1 < len(qurl) {
		return qurl[i+1:]
	}
	return qurl
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
