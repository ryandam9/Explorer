package xref

import (
	"context"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsapigw "github.com/aws/aws-sdk-go-v2/service/apigateway"
	awsapigwv2 "github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	awscf "github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awselbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	awsroute53 "github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/ryandam9/aws_explorer/internal/model"
)

// Networking edge extractors (#341). Regional services (ELBv2 target groups,
// API Gateway, VPC endpoints) run in collectRegion; the global services
// (CloudFront, Route 53) run once in collectGlobalNetworking, stamped against
// their home region (§3).

// --- ELBv2 load balancer + target groups --------------------------------------

// elbLoadBalancerEdges maps a load balancer to its security groups and subnets.
func elbLoadBalancerEdges(lb elbv2types.LoadBalancer, region string) []Edge {
	from := Reference{Service: "elbv2", Type: "load-balancer", Region: region,
		ID: aws.ToString(lb.LoadBalancerArn), Name: aws.ToString(lb.LoadBalancerName)}
	var edges []Edge
	for _, g := range lb.SecurityGroups {
		edges = append(edges, Edge{From: withVia(from, "load balancer security group"), Target: g})
	}
	for _, az := range lb.AvailabilityZones {
		if s := aws.ToString(az.SubnetId); s != "" {
			edges = append(edges, Edge{From: withVia(from, "load balancer subnet"), Target: s})
		}
	}
	return edges
}

// targetGroupTargetEdges maps a target group's registered targets (instances,
// IPs, or Lambda functions) to "target group → target" edges.
func targetGroupTargetEdges(tgRef Reference, targets []elbv2types.TargetHealthDescription) []Edge {
	var edges []Edge
	for _, th := range targets {
		if th.Target == nil {
			continue
		}
		if id := aws.ToString(th.Target.Id); id != "" {
			edges = append(edges, Edge{From: withVia(tgRef, "registered target"), Target: id})
		}
	}
	return edges
}

// elbTargetGroupEdges pages target groups, links each to its load balancer(s)
// and registered targets.
func elbTargetGroupEdges(ctx context.Context, client *awselbv2.Client, region string, rec *recorder) []Edge {
	var edges []Edge
	p := awselbv2.NewDescribeTargetGroupsPaginator(client, &awselbv2.DescribeTargetGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("elbv2", err)
			break
		}
		for _, tg := range page.TargetGroups {
			tgRef := Reference{Service: "elbv2", Type: "target-group", Region: region,
				ID: aws.ToString(tg.TargetGroupArn), Name: aws.ToString(tg.TargetGroupName)}
			for _, lbArn := range tg.LoadBalancerArns {
				edges = append(edges, Edge{From: withVia(tgRef, "attached to load balancer"), Target: lbArn})
			}
			health, err := client.DescribeTargetHealth(ctx, &awselbv2.DescribeTargetHealthInput{TargetGroupArn: tg.TargetGroupArn})
			if err != nil {
				rec.record("elbv2", err)
				continue
			}
			edges = append(edges, targetGroupTargetEdges(tgRef, health.TargetHealthDescriptions)...)
		}
	}
	return edges
}

// --- API Gateway --------------------------------------------------------------

var lambdaARNPattern = regexp.MustCompile(`arn:aws[a-z0-9-]*:lambda:[a-z0-9-]+:[0-9]+:function:[A-Za-z0-9_-]+`)

// extractLambdaARN pulls a Lambda function ARN out of an API Gateway
// integration/authorizer URI (which embeds it), "" if none.
func extractLambdaARN(uri string) string { return lambdaARNPattern.FindString(uri) }

func apiGatewayEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	var edges []Edge
	edges = append(edges, apiGatewayV2Edges(ctx, awsapigwv2.NewFromConfig(cfg), region, rec)...)
	edges = append(edges, apiGatewayV1Edges(ctx, awsapigw.NewFromConfig(cfg), region, rec)...)
	return edges
}

// apiGatewayV2Edges maps HTTP/WebSocket APIs to their Lambda integrations and
// authorizers, and VPC links to their subnets/security groups.
func apiGatewayV2Edges(ctx context.Context, client *awsapigwv2.Client, region string, rec *recorder) []Edge {
	var edges []Edge
	apis, err := client.GetApis(ctx, &awsapigwv2.GetApisInput{})
	if err != nil {
		rec.record("apigateway", err)
	} else {
		for _, api := range apis.Items {
			id := aws.ToString(api.ApiId)
			from := Reference{Service: "apigateway", Type: "http-api", Region: region, ID: id, Name: aws.ToString(api.Name)}

			if ints, err := client.GetIntegrations(ctx, &awsapigwv2.GetIntegrationsInput{ApiId: api.ApiId}); err != nil {
				rec.record("apigateway", err)
			} else {
				for _, in := range ints.Items {
					if arn := extractLambdaARN(aws.ToString(in.IntegrationUri)); arn != "" {
						edges = append(edges, Edge{From: withVia(from, "Lambda integration"), Target: arn})
					}
				}
			}
			if auths, err := client.GetAuthorizers(ctx, &awsapigwv2.GetAuthorizersInput{ApiId: api.ApiId}); err != nil {
				rec.record("apigateway", err)
			} else {
				for _, a := range auths.Items {
					if arn := extractLambdaARN(aws.ToString(a.AuthorizerUri)); arn != "" {
						edges = append(edges, Edge{From: withVia(from, "Lambda authorizer"), Target: arn})
					}
				}
			}
		}
	}

	links, err := client.GetVpcLinks(ctx, &awsapigwv2.GetVpcLinksInput{})
	if err != nil {
		rec.record("apigateway", err)
	} else {
		for _, vl := range links.Items {
			from := Reference{Service: "apigateway", Type: "vpc-link", Region: region,
				ID: aws.ToString(vl.VpcLinkId), Name: aws.ToString(vl.Name)}
			for _, s := range vl.SubnetIds {
				edges = append(edges, Edge{From: withVia(from, "VPC link subnet"), Target: s})
			}
			for _, g := range vl.SecurityGroupIds {
				edges = append(edges, Edge{From: withVia(from, "VPC link security group"), Target: g})
			}
		}
	}
	return edges
}

// apiGatewayV1Edges maps REST API authorizers to their Lambda functions.
// (Per-method backend integrations are deferred — they require a resource×method
// walk; see #341 follow-up.)
func apiGatewayV1Edges(ctx context.Context, client *awsapigw.Client, region string, rec *recorder) []Edge {
	var edges []Edge
	apis, err := client.GetRestApis(ctx, &awsapigw.GetRestApisInput{})
	if err != nil {
		rec.record("apigateway", err)
		return edges
	}
	for _, api := range apis.Items {
		from := Reference{Service: "apigateway", Type: "rest-api", Region: region,
			ID: aws.ToString(api.Id), Name: aws.ToString(api.Name)}
		auths, err := client.GetAuthorizers(ctx, &awsapigw.GetAuthorizersInput{RestApiId: api.Id})
		if err != nil {
			rec.record("apigateway", err)
			continue
		}
		for _, a := range auths.Items {
			if arn := extractLambdaARN(aws.ToString(a.AuthorizerUri)); arn != "" {
				edges = append(edges, Edge{From: withVia(from, "Lambda authorizer"), Target: arn})
			}
		}
	}
	return edges
}

// --- VPC endpoints ------------------------------------------------------------

func vpcEndpointEdges(ep ec2types.VpcEndpoint, region string) []Edge {
	from := Reference{Service: "ec2", Type: "vpc-endpoint", Region: region,
		ID: aws.ToString(ep.VpcEndpointId), Name: aws.ToString(ep.ServiceName)}
	var edges []Edge
	if svc := aws.ToString(ep.ServiceName); svc != "" {
		edges = append(edges, Edge{From: withVia(from, "endpoint service"), Target: svc})
	}
	for _, s := range ep.SubnetIds {
		edges = append(edges, Edge{From: withVia(from, "endpoint subnet"), Target: s})
	}
	for _, g := range ep.Groups {
		if id := aws.ToString(g.GroupId); id != "" {
			edges = append(edges, Edge{From: withVia(from, "endpoint security group"), Target: id})
		}
	}
	return edges
}

func vpcEndpointsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsec2.NewFromConfig(cfg)
	var edges []Edge
	p := awsec2.NewDescribeVpcEndpointsPaginator(client, &awsec2.DescribeVpcEndpointsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("ec2", err)
			break
		}
		for _, ep := range page.VpcEndpoints {
			edges = append(edges, vpcEndpointEdges(ep, region)...)
		}
	}
	return edges
}

// --- CloudFront (global) ------------------------------------------------------

// cloudFrontDistributionEdges maps a distribution to its origins (by DNS name),
// viewer ACM certificate, WAF web ACL, and origin access control.
func cloudFrontDistributionEdges(d cftypes.DistributionSummary) []Edge {
	name := aws.ToString(d.Id)
	if d.Aliases != nil && len(d.Aliases.Items) > 0 {
		name = d.Aliases.Items[0]
	}
	from := Reference{Service: "cloudfront", Type: "distribution", Region: "global",
		ID: aws.ToString(d.ARN), Name: name}
	var edges []Edge

	if d.Origins != nil {
		for _, o := range d.Origins.Items {
			if dn := aws.ToString(o.DomainName); dn != "" {
				edges = append(edges, Edge{From: withVia(from, "origin (DNS-derived)"), Target: dn})
			}
			if oac := aws.ToString(o.OriginAccessControlId); oac != "" {
				edges = append(edges, Edge{From: withVia(from, "origin access control"), Target: oac})
			}
		}
	}
	if vc := d.ViewerCertificate; vc != nil {
		if arn := aws.ToString(vc.ACMCertificateArn); arn != "" {
			edges = append(edges, Edge{From: withVia(from, "viewer certificate"), Target: arn})
		}
	}
	if acl := aws.ToString(d.WebACLId); acl != "" {
		edges = append(edges, Edge{From: withVia(from, "WAF web ACL"), Target: acl})
	}
	return edges
}

func cloudFrontEdges(ctx context.Context, cfg aws.Config, rec *recorder) []Edge {
	client := awscf.NewFromConfig(cfg)
	var edges []Edge
	p := awscf.NewListDistributionsPaginator(client, &awscf.ListDistributionsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("cloudfront", err)
			break
		}
		if page.DistributionList == nil {
			continue
		}
		for _, d := range page.DistributionList.Items {
			edges = append(edges, cloudFrontDistributionEdges(d)...)
		}
	}
	return edges
}

// --- Route 53 (global) --------------------------------------------------------

// route53AliasEdges maps a hosted zone's alias records to their (DNS-named)
// targets — ALB/CloudFront/S3/API Gateway, identified by DNS name not ARN.
func route53AliasEdges(records []r53Record, zone string) []Edge {
	var edges []Edge
	for _, r := range records {
		if r.aliasTarget == "" {
			continue
		}
		from := Reference{Service: "route53", Type: "record", Region: "global",
			ID: zone + "/" + r.name, Name: r.name}
		edges = append(edges, Edge{From: withVia(from, "alias target (DNS-derived)"), Target: r.aliasTarget})
	}
	return edges
}

// r53Record is the minimal alias-record shape route53AliasEdges needs (kept
// SDK-free so it is trivially testable).
type r53Record struct {
	name        string
	aliasTarget string
}

func route53Edges(ctx context.Context, cfg aws.Config, rec *recorder) []Edge {
	client := awsroute53.NewFromConfig(cfg)
	var edges []Edge
	zp := awsroute53.NewListHostedZonesPaginator(client, &awsroute53.ListHostedZonesInput{})
	for zp.HasMorePages() {
		page, err := zp.NextPage(ctx)
		if err != nil {
			rec.record("route53", err)
			break
		}
		for _, z := range page.HostedZones {
			zoneName := aws.ToString(z.Name)
			rp := awsroute53.NewListResourceRecordSetsPaginator(client, &awsroute53.ListResourceRecordSetsInput{HostedZoneId: z.Id})
			for rp.HasMorePages() {
				rPage, err := rp.NextPage(ctx)
				if err != nil {
					rec.record("route53", err)
					break
				}
				var recs []r53Record
				for _, rr := range rPage.ResourceRecordSets {
					if rr.AliasTarget != nil {
						recs = append(recs, r53Record{name: aws.ToString(rr.Name), aliasTarget: aws.ToString(rr.AliasTarget.DNSName)})
					}
				}
				edges = append(edges, route53AliasEdges(recs, zoneName)...)
			}
		}
	}
	return edges
}

// collectGlobalNetworking runs the global networking services (CloudFront,
// Route 53) once, against us-east-1.
func collectGlobalNetworking(ctx context.Context, baseCfg aws.Config, timeout time.Duration) ([]Edge, []model.ExploreError) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	cfg := baseCfg
	cfg.Region = "us-east-1"
	rec := &recorder{region: "global"}
	var edges []Edge
	edges = append(edges, cloudFrontEdges(ctx, cfg, rec)...)
	edges = append(edges, route53Edges(ctx, cfg, rec)...)
	return edges, rec.errs
}
