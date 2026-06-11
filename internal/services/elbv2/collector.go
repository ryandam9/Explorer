package elbv2

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/services"
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
		resources = input.EmitOrAppend(resources, batch)
	}

	return resources, nil
}

func (c *Collector) mapLoadBalancer(region string, lb types.LoadBalancer, detail services.DetailLevel) model.Resource {
	id := aws.ToString(lb.LoadBalancerArn)
	name := aws.ToString(lb.LoadBalancerName)
	state := string(lb.State.Code)

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
