package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// viaTargets collapses edges to a via→target map for compact assertions.
func viaTargets(edges []Edge) map[string]string {
	m := make(map[string]string, len(edges))
	for _, e := range edges {
		m[e.From.Via] = e.Target
	}
	return m
}

func TestLambdaFunctionEdges(t *testing.T) {
	fn := lambdatypes.FunctionConfiguration{
		FunctionName: aws.String("checkout"),
		FunctionArn:  aws.String("arn:aws:lambda:us-east-1:111:function:checkout"),
		Role:         aws.String("arn:aws:iam::111:role/app"),
		KMSKeyArn:    aws.String("arn:aws:kms:us-east-1:111:key/k"),
		VpcConfig: &lambdatypes.VpcConfigResponse{
			SubnetIds:        []string{"subnet-1"},
			SecurityGroupIds: []string{"sg-1"},
		},
		Layers:           []lambdatypes.Layer{{Arn: aws.String("arn:aws:lambda:us-east-1:111:layer:util:3")}},
		DeadLetterConfig: &lambdatypes.DeadLetterConfig{TargetArn: aws.String("arn:aws:sqs:us-east-1:111:dlq")},
	}
	got := viaTargets(lambdaFunctionEdges(fn, "us-east-1"))
	want := map[string]string{
		"execution role":                       "arn:aws:iam::111:role/app",
		"environment encryption key":           "arn:aws:kms:us-east-1:111:key/k",
		"VPC subnet":                           "subnet-1",
		"VPC security group":                   "sg-1",
		"layer":                                "arn:aws:lambda:us-east-1:111:layer:util:3",
		"dead-letter target":                   "arn:aws:sqs:us-east-1:111:dlq",
		"CloudWatch log group (by convention)": "/aws/lambda/checkout",
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

func TestEventSourceEdges(t *testing.T) {
	mappings := []lambdatypes.EventSourceMappingConfiguration{
		{EventSourceArn: aws.String("arn:aws:sqs:us-east-1:111:orders"), FunctionArn: aws.String("arn:aws:lambda:us-east-1:111:function:worker")},
		{EventSourceArn: aws.String(""), FunctionArn: aws.String("arn:aws:lambda:us-east-1:111:function:x")}, // skipped
	}
	edges := eventSourceEdges(mappings, "us-east-1")
	if len(edges) != 1 {
		t.Fatalf("want 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.From.Service != "lambda" || e.From.ID != "arn:aws:lambda:us-east-1:111:function:worker" {
		t.Errorf("from = %+v", e.From)
	}
	if e.From.Via != "event source" || e.Target != "arn:aws:sqs:us-east-1:111:orders" {
		t.Errorf("edge = %+v", e)
	}
}

func TestEventSourceEdge_ResolvesBothDirections(t *testing.T) {
	edges := eventSourceEdges([]lambdatypes.EventSourceMappingConfiguration{
		{EventSourceArn: aws.String("arn:aws:sqs:us-east-1:111:orders"), FunctionArn: aws.String("arn:aws:lambda:us-east-1:111:function:worker")},
	}, "us-east-1")
	fwd, rev := BuildForwardIndex(edges), BuildIndex(edges)

	// related(queue).UsedBy → the Lambda
	q := Related("arn:aws:sqs:us-east-1:111:orders", fwd, rev, 1)
	if len(q.UsedBy) != 1 || q.UsedBy[0].Service != "lambda" {
		t.Fatalf("queue.UsedBy = %+v", q.UsedBy)
	}
	// related(lambda).Uses → the queue
	l := Related("arn:aws:lambda:us-east-1:111:function:worker", fwd, rev, 1)
	if len(l.Uses) != 1 || l.Uses[0].Service != "sqs" {
		t.Fatalf("lambda.Uses = %+v", l.Uses)
	}
}

func TestEC2InstanceEdges(t *testing.T) {
	profiles := map[string][]string{
		"arn:aws:iam::111:instance-profile/app": {"arn:aws:iam::111:role/app"},
	}
	inst := ec2types.Instance{
		InstanceId:         aws.String("i-1"),
		SubnetId:           aws.String("subnet-9"),
		ImageId:            aws.String("ami-abc"),
		KeyName:            aws.String("ops-key"),
		IamInstanceProfile: &ec2types.IamInstanceProfile{Arn: aws.String("arn:aws:iam::111:instance-profile/app")},
		NetworkInterfaces:  []ec2types.InstanceNetworkInterface{{NetworkInterfaceId: aws.String("eni-7")}},
	}
	got := viaTargets(ec2InstanceEdges(inst, "us-east-1", profiles))
	if got["subnet"] != "subnet-9" || got["AMI"] != "ami-abc" || got["key pair"] != "ops-key" {
		t.Errorf("scalar edges = %+v", got)
	}
	if got["attached network interface"] != "eni-7" {
		t.Errorf("eni edge missing: %+v", got)
	}
	if got["instance profile app"] != "arn:aws:iam::111:role/app" {
		t.Errorf("instance-profile role edge = %+v", got)
	}
}

func TestEIPEdges(t *testing.T) {
	addrs := []ec2types.Address{
		{AllocationId: aws.String("eipalloc-1"), InstanceId: aws.String("i-1")},
		{AllocationId: aws.String("eipalloc-2"), NetworkInterfaceId: aws.String("eni-2")},
		{AllocationId: aws.String("eipalloc-3")}, // unassociated → skipped
		{InstanceId: aws.String("i-9")},          // no allocation id → skipped
	}
	edges := eipEdges(addrs, "us-east-1")
	if len(edges) != 2 {
		t.Fatalf("want 2 edges, got %d: %+v", len(edges), edges)
	}
	if edges[0].From.ID != "i-1" || edges[0].Target != "eipalloc-1" {
		t.Errorf("edge0 = %+v", edges[0])
	}
	if edges[1].From.Type != "network-interface" || edges[1].Target != "eipalloc-2" {
		t.Errorf("edge1 = %+v", edges[1])
	}
}

func TestECSTaskDefEdges(t *testing.T) {
	td := ecstypes.TaskDefinition{
		TaskDefinitionArn: aws.String("arn:aws:ecs:us-east-1:111:task-definition/web:5"),
		Family:            aws.String("web"),
		TaskRoleArn:       aws.String("arn:aws:iam::111:role/task"),
		ExecutionRoleArn:  aws.String("arn:aws:iam::111:role/exec"),
		ContainerDefinitions: []ecstypes.ContainerDefinition{
			{
				LogConfiguration: &ecstypes.LogConfiguration{Options: map[string]string{"awslogs-group": "/ecs/web"}},
				Secrets:          []ecstypes.Secret{{Name: aws.String("DB"), ValueFrom: aws.String("arn:aws:secretsmanager:us-east-1:111:secret:db")}},
			},
		},
	}
	got := viaTargets(ecsTaskDefEdges(td, "us-east-1"))
	want := map[string]string{
		"ECS task role":       "arn:aws:iam::111:role/task",
		"ECS execution role":  "arn:aws:iam::111:role/exec",
		"container log group": "/ecs/web",
		"container secret":    "arn:aws:secretsmanager:us-east-1:111:secret:db",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
}

func TestEKSClusterEdges(t *testing.T) {
	cluster := ekstypes.Cluster{
		Arn:     aws.String("arn:aws:eks:us-east-1:111:cluster/prod"),
		Name:    aws.String("prod"),
		RoleArn: aws.String("arn:aws:iam::111:role/eks"),
		ResourcesVpcConfig: &ekstypes.VpcConfigResponse{
			ClusterSecurityGroupId: aws.String("sg-cluster"),
			SecurityGroupIds:       []string{"sg-extra"},
			SubnetIds:              []string{"subnet-a"},
		},
		Identity: &ekstypes.Identity{Oidc: &ekstypes.OIDC{Issuer: aws.String("https://oidc.eks.us-east-1.amazonaws.com/id/ABC")}},
	}
	got := viaTargets(eksClusterEdges(cluster, "us-east-1"))
	want := map[string]string{
		"EKS cluster role":          "arn:aws:iam::111:role/eks",
		"cluster security group":    "sg-cluster",
		"additional security group": "sg-extra",
		"subnet":                    "subnet-a",
		"OIDC provider":             "https://oidc.eks.us-east-1.amazonaws.com/id/ABC",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
}

func TestReferenceFromIdentifier_BareIDs(t *testing.T) {
	cases := map[string]struct{ service, typ string }{
		"sg-1":            {"ec2", "security-group"},
		"subnet-1":        {"ec2", "subnet"},
		"ami-1":           {"ec2", "image"},
		"eipalloc-1":      {"ec2", "elastic-ip"},
		"/aws/lambda/foo": {"logs", "log-group"},
		"/ecs/web":        {"logs", "log-group"},
		"ops-key":         {"", ""}, // unrecognized bare id
	}
	for id, want := range cases {
		r := referenceFromIdentifier(id)
		if r.Service != want.service || r.Type != want.typ {
			t.Errorf("referenceFromIdentifier(%q) = {%q,%q}, want {%q,%q}", id, r.Service, r.Type, want.service, want.typ)
		}
	}
}
