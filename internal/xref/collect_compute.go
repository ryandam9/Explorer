package xref

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// Compute edge extractors (#338). The per-resource mapping is split into pure
// functions over the SDK structs so each can be unit-tested with a fixture; the
// AWS-calling wrappers in collect.go just page and delegate.

// --- Lambda -------------------------------------------------------------------

// lambdaFunctionEdges maps one function's configuration to its reference edges:
// execution role, environment KMS key, VPC subnets/security groups, layers,
// dead-letter target, and (by the documented naming convention) its CloudWatch
// log group. Event-source mappings come from a separate account-wide list.
func lambdaFunctionEdges(fn lambdatypes.FunctionConfiguration, region string) []Edge {
	name := aws.ToString(fn.FunctionName)
	from := Reference{Service: "lambda", Type: "function", Region: region,
		ID: aws.ToString(fn.FunctionArn), Name: name}
	var edges []Edge

	if role := aws.ToString(fn.Role); role != "" {
		edges = append(edges, Edge{From: withVia(from, "execution role"), Target: role})
	}
	if key := aws.ToString(fn.KMSKeyArn); key != "" {
		edges = append(edges, Edge{From: withVia(from, "environment encryption key"), Target: key})
	}
	if vc := fn.VpcConfig; vc != nil {
		for _, s := range vc.SubnetIds {
			edges = append(edges, Edge{From: withVia(from, "VPC subnet"), Target: s})
		}
		for _, g := range vc.SecurityGroupIds {
			edges = append(edges, Edge{From: withVia(from, "VPC security group"), Target: g})
		}
	}
	for _, l := range fn.Layers {
		if arn := aws.ToString(l.Arn); arn != "" {
			edges = append(edges, Edge{From: withVia(from, "layer"), Target: arn})
		}
	}
	if dlq := fn.DeadLetterConfig; dlq != nil {
		if t := aws.ToString(dlq.TargetArn); t != "" {
			edges = append(edges, Edge{From: withVia(from, "dead-letter target"), Target: t})
		}
	}
	if name != "" {
		// Log group is derived from the documented convention (overlaps AXE-011);
		// labelled so it reads as inferred, not observed.
		edges = append(edges, Edge{From: withVia(from, "CloudWatch log group (by convention)"), Target: "/aws/lambda/" + name})
	}
	return edges
}

// eventSourceEdges maps Lambda event-source mappings to "consumes from" edges
// (SQS / DynamoDB streams / Kinesis / MSK → function). These make the
// queue↔Lambda / stream↔Lambda links resolvable from both directions.
func eventSourceEdges(mappings []lambdatypes.EventSourceMappingConfiguration, region string) []Edge {
	var edges []Edge
	for _, m := range mappings {
		src := aws.ToString(m.EventSourceArn)
		fnArn := aws.ToString(m.FunctionArn)
		if src == "" || fnArn == "" {
			continue
		}
		from := Reference{Service: "lambda", Type: "function", Region: region,
			ID: fnArn, Name: lastSegment(shortForm(fnArn))}
		edges = append(edges, Edge{From: withVia(from, "event source"), Target: src})
	}
	return edges
}

// --- EC2 ----------------------------------------------------------------------

// ec2InstanceEdges maps one instance to its IAM instance-profile role(s),
// subnet, AMI, key pair, and attached network interfaces. (Volume → KMS/instance
// and ENI → security group are collected from their own describe calls.)
func ec2InstanceEdges(inst ec2types.Instance, region string, profiles map[string][]string) []Edge {
	from := Reference{Service: "ec2", Type: "instance", Region: region,
		ID: aws.ToString(inst.InstanceId), Name: nameTag(inst.Tags)}
	var edges []Edge

	if inst.IamInstanceProfile != nil {
		profileArn := aws.ToString(inst.IamInstanceProfile.Arn)
		for _, role := range profiles[profileArn] {
			edges = append(edges, Edge{From: withVia(from, "instance profile "+shortForm(profileArn)), Target: role})
		}
	}
	if s := aws.ToString(inst.SubnetId); s != "" {
		edges = append(edges, Edge{From: withVia(from, "subnet"), Target: s})
	}
	if img := aws.ToString(inst.ImageId); img != "" {
		edges = append(edges, Edge{From: withVia(from, "AMI"), Target: img})
	}
	if k := aws.ToString(inst.KeyName); k != "" {
		edges = append(edges, Edge{From: withVia(from, "key pair"), Target: k})
	}
	for _, ni := range inst.NetworkInterfaces {
		if id := aws.ToString(ni.NetworkInterfaceId); id != "" {
			edges = append(edges, Edge{From: withVia(from, "attached network interface"), Target: id})
		}
	}
	return edges
}

// eipEdges maps Elastic IP associations to the instance (or ENI) they serve.
func eipEdges(addrs []ec2types.Address, region string) []Edge {
	var edges []Edge
	for _, a := range addrs {
		alloc := aws.ToString(a.AllocationId)
		if alloc == "" {
			continue
		}
		switch {
		case aws.ToString(a.InstanceId) != "":
			from := Reference{Service: "ec2", Type: "instance", Region: region, ID: aws.ToString(a.InstanceId)}
			edges = append(edges, Edge{From: withVia(from, "associated Elastic IP"), Target: alloc})
		case aws.ToString(a.NetworkInterfaceId) != "":
			from := Reference{Service: "ec2", Type: "network-interface", Region: region, ID: aws.ToString(a.NetworkInterfaceId)}
			edges = append(edges, Edge{From: withVia(from, "associated Elastic IP"), Target: alloc})
		}
	}
	return edges
}

// eipAddresses is the narrow EC2 surface eipEdges' wrapper needs.
func collectEIPEdges(ctx context.Context, client *awsec2.Client, region string, rec *recorder) []Edge {
	out, err := client.DescribeAddresses(ctx, &awsec2.DescribeAddressesInput{})
	if err != nil {
		rec.record("ec2", err)
		return nil
	}
	return eipEdges(out.Addresses, region)
}

// --- ECS ----------------------------------------------------------------------

// ecsTaskDefEdges maps a task definition to its task/execution roles, the
// CloudWatch log groups its containers log to, and any Secrets Manager / SSM
// parameters referenced (the ARNs, never the values — §14).
func ecsTaskDefEdges(td ecstypes.TaskDefinition, region string) []Edge {
	from := Reference{Service: "ecs", Type: "task-definition", Region: region,
		ID: aws.ToString(td.TaskDefinitionArn), Name: aws.ToString(td.Family)}
	var edges []Edge

	if role := aws.ToString(td.TaskRoleArn); role != "" {
		edges = append(edges, Edge{From: withVia(from, "ECS task role"), Target: role})
	}
	if role := aws.ToString(td.ExecutionRoleArn); role != "" {
		edges = append(edges, Edge{From: withVia(from, "ECS execution role"), Target: role})
	}
	for _, c := range td.ContainerDefinitions {
		if lc := c.LogConfiguration; lc != nil {
			if g, ok := lc.Options["awslogs-group"]; ok && g != "" {
				edges = append(edges, Edge{From: withVia(from, "container log group"), Target: g})
			}
		}
		for _, s := range c.Secrets {
			if v := aws.ToString(s.ValueFrom); v != "" {
				edges = append(edges, Edge{From: withVia(from, "container secret"), Target: v})
			}
		}
	}
	return edges
}

// --- EKS ----------------------------------------------------------------------

// eksClusterEdges maps a cluster to its IAM role, control-plane security
// groups, subnets, and OIDC provider issuer.
func eksClusterEdges(cluster ekstypes.Cluster, region string) []Edge {
	from := Reference{Service: "eks", Type: "cluster", Region: region,
		ID: aws.ToString(cluster.Arn), Name: aws.ToString(cluster.Name)}
	var edges []Edge

	if role := aws.ToString(cluster.RoleArn); role != "" {
		edges = append(edges, Edge{From: withVia(from, "EKS cluster role"), Target: role})
	}
	if vc := cluster.ResourcesVpcConfig; vc != nil {
		if g := aws.ToString(vc.ClusterSecurityGroupId); g != "" {
			edges = append(edges, Edge{From: withVia(from, "cluster security group"), Target: g})
		}
		for _, g := range vc.SecurityGroupIds {
			edges = append(edges, Edge{From: withVia(from, "additional security group"), Target: g})
		}
		for _, s := range vc.SubnetIds {
			edges = append(edges, Edge{From: withVia(from, "subnet"), Target: s})
		}
	}
	if id := cluster.Identity; id != nil && id.Oidc != nil {
		if iss := aws.ToString(id.Oidc.Issuer); iss != "" {
			edges = append(edges, Edge{From: withVia(from, "OIDC provider"), Target: iss})
		}
	}
	return edges
}

// listEventSourceEdges pages account-wide event-source mappings and maps them.
func listEventSourceEdges(ctx context.Context, client *awslambda.Client, region string, rec *recorder) []Edge {
	var edges []Edge
	p := awslambda.NewListEventSourceMappingsPaginator(client, &awslambda.ListEventSourceMappingsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("lambda", err)
			break
		}
		edges = append(edges, eventSourceEdges(page.EventSourceMappings, region)...)
	}
	return edges
}
