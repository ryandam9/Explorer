package elbv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

type Collector struct{}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "elbv2"
}

func (c *Collector) IsGlobal() bool {
	return false
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	client := elasticloadbalancingv2.NewFromConfig(input.AWSConfig)
	var resources []model.Resource

	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return resources, fmt.Errorf("failed to describe load balancers: %w", err)
		}

		batch := make([]model.Resource, 0, len(page.LoadBalancers))
		for _, lb := range page.LoadBalancers {
			batch = append(batch, c.mapLoadBalancer(input.Region, lb, input.DetailLevel))
		}
		// Tags aren't part of DescribeLoadBalancers; enrich the page with one
		// batched DescribeTags sweep (best-effort — a tag failure must not drop
		// the load balancers themselves).
		c.applyTags(ctx, client, batch)
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

// describeTagsBatch is the DescribeTags ResourceArns limit (20 per call).
const describeTagsBatch = 20

// applyTags fills each resource's Tags from DescribeTags, in batches of 20
// ARNs. Errors are swallowed: tags are an enrichment, not a reason to fail the
// whole collection.
func (c *Collector) applyTags(ctx context.Context, client *elasticloadbalancingv2.Client, batch []model.Resource) {
	byARN := make(map[string]int, len(batch))
	arns := make([]string, 0, len(batch))
	for i, r := range batch {
		if r.ARN == "" {
			continue
		}
		byARN[r.ARN] = i
		arns = append(arns, r.ARN)
	}
	for start := 0; start < len(arns); start += describeTagsBatch {
		chunk := arns[start:min(start+describeTagsBatch, len(arns))]
		out, err := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: chunk})
		if err != nil {
			return
		}
		for _, td := range out.TagDescriptions {
			idx, ok := byARN[aws.ToString(td.ResourceArn)]
			if !ok {
				continue
			}
			for _, t := range td.Tags {
				batch[idx].Tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
		}
	}
}

func (c *Collector) mapLoadBalancer(region string, lb types.LoadBalancer, detail services.DetailLevel) model.Resource {
	id := aws.ToString(lb.LoadBalancerArn)
	name := aws.ToString(lb.LoadBalancerName)
	state := ""
	if lb.State != nil {
		state = string(lb.State.Code)
	}

	tags := make(map[string]string)

	res := model.Resource{
		Service: "elbv2",
		Type:    "loadbalancer",
		Region:  region,
		ID:      id,
		Name:    name,
		ARN:     id,
		State:   state,
		Tags:    tags,
		Summary: map[string]string{
			"type":    string(lb.Type),
			"scheme":  string(lb.Scheme),
			"dnsName": aws.ToString(lb.DNSName),
			"vpcId":   aws.ToString(lb.VpcId),
			"ipType":  string(lb.IpAddressType),
		},
	}

	if lb.CreatedTime != nil {
		res.CreatedAt = lb.CreatedTime
	}

	return res
}
