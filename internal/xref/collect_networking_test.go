package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

func TestELBLoadBalancerEdges(t *testing.T) {
	lb := elbv2types.LoadBalancer{
		LoadBalancerArn:   aws.String("arn:aws:elasticloadbalancing:us-east-1:111:loadbalancer/app/web/abc"),
		LoadBalancerName:  aws.String("web"),
		SecurityGroups:    []string{"sg-1", "sg-2"},
		AvailabilityZones: []elbv2types.AvailabilityZone{{SubnetId: aws.String("subnet-1")}, {SubnetId: aws.String("subnet-2")}},
	}
	edges := elbLoadBalancerEdges(lb, "us-east-1")
	if len(edges) != 4 {
		t.Fatalf("want 4 edges, got %d: %+v", len(edges), edges)
	}
	var sgs, subnets int
	for _, e := range edges {
		switch e.From.Via {
		case "load balancer security group":
			sgs++
		case "load balancer subnet":
			subnets++
		}
		if e.From.Type != "load-balancer" {
			t.Errorf("from type = %q", e.From.Type)
		}
	}
	if sgs != 2 || subnets != 2 {
		t.Errorf("sgs=%d subnets=%d", sgs, subnets)
	}
}

func TestTargetGroupTargetEdges_BothDirections(t *testing.T) {
	tgRef := Reference{Service: "elbv2", Type: "target-group", Region: "us-east-1",
		ID: "arn:aws:elasticloadbalancing:us-east-1:111:targetgroup/web/abc", Name: "web"}
	targets := []elbv2types.TargetHealthDescription{
		{Target: &elbv2types.TargetDescription{Id: aws.String("arn:aws:lambda:us-east-1:111:function:handler")}},
		{Target: &elbv2types.TargetDescription{Id: aws.String("i-123")}},
		{Target: nil}, // skipped
	}
	edges := targetGroupTargetEdges(tgRef, targets)
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(edges), edges)
	}
	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)
	// related(lambda).UsedBy → the target group
	lam := Related("arn:aws:lambda:us-east-1:111:function:handler", fwd, rev, 1, false)
	if len(lam.UsedBy) != 1 || lam.UsedBy[0].Type != "target-group" {
		t.Fatalf("lambda.UsedBy = %+v", lam.UsedBy)
	}
}

func TestExtractLambdaARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/arn:aws:lambda:us-east-1:111:function:auth/invocations": "arn:aws:lambda:us-east-1:111:function:auth",
		"arn:aws:lambda:us-east-1:111:function:proxy": "arn:aws:lambda:us-east-1:111:function:proxy",
		"http://example.com":                          "",
	}
	for uri, want := range cases {
		if got := extractLambdaARN(uri); got != want {
			t.Errorf("extractLambdaARN(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestCloudFrontDistributionEdges(t *testing.T) {
	d := cftypes.DistributionSummary{
		Id:       aws.String("E123"),
		ARN:      aws.String("arn:aws:cloudfront::111:distribution/E123"),
		Aliases:  &cftypes.Aliases{Items: []string{"cdn.example.com"}},
		Origins:  &cftypes.Origins{Items: []cftypes.Origin{{DomainName: aws.String("bucket.s3.amazonaws.com"), OriginAccessControlId: aws.String("OAC1")}}},
		WebACLId: aws.String("arn:aws:wafv2:us-east-1:111:global/webacl/site/abc"),
		ViewerCertificate: &cftypes.ViewerCertificate{
			ACMCertificateArn: aws.String("arn:aws:acm:us-east-1:111:certificate/xyz"),
		},
	}
	got := viaTargets(cloudFrontDistributionEdges(d))
	want := map[string]string{
		"origin (DNS-derived)":  "bucket.s3.amazonaws.com",
		"origin access control": "OAC1",
		"WAF web ACL":           "arn:aws:wafv2:us-east-1:111:global/webacl/site/abc",
		"viewer certificate":    "arn:aws:acm:us-east-1:111:certificate/xyz",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
	if len(got) != len(want) {
		t.Errorf("edge count = %d, want %d (%+v)", len(got), len(want), got)
	}
}

func TestRoute53AliasEdges(t *testing.T) {
	records := []r53Record{
		{name: "app.example.com.", aliasTarget: "web-123.us-east-1.elb.amazonaws.com."},
		{name: "static.example.com.", aliasTarget: ""}, // non-alias → skipped
	}
	edges := route53AliasEdges(records, "Z1/example.com.")
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.From.Service != "route53" || e.Target != "web-123.us-east-1.elb.amazonaws.com." {
		t.Errorf("edge = %+v", e)
	}
	if e.From.Via != "alias target (DNS-derived)" {
		t.Errorf("via = %q", e.From.Via)
	}
}

func TestVpcEndpointEdges(t *testing.T) {
	ep := ec2types.VpcEndpoint{
		VpcEndpointId: aws.String("vpce-1"),
		ServiceName:   aws.String("com.amazonaws.us-east-1.s3"),
		SubnetIds:     []string{"subnet-1"},
		Groups:        []ec2types.SecurityGroupIdentifier{{GroupId: aws.String("sg-9")}},
	}
	got := viaTargets(vpcEndpointEdges(ep, "us-east-1"))
	want := map[string]string{
		"endpoint service":        "com.amazonaws.us-east-1.s3",
		"endpoint subnet":         "subnet-1",
		"endpoint security group": "sg-9",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
}

func TestCheckedTypes_NetworkingRegistered(t *testing.T) {
	acm := CheckedTypes(KindACMCert)
	sg := CheckedTypes(KindSecurityGroup)
	if !contains(acm, "CloudFront distribution viewer certificates") {
		t.Errorf("ACM CheckedTypes missing CloudFront: %v", acm)
	}
	if !contains(sg, "Load balancer security groups") || !contains(sg, "VPC endpoint security groups") {
		t.Errorf("SG CheckedTypes missing networking entries: %v", sg)
	}
}
