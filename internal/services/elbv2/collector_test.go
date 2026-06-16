package elbv2

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "elbv2" {
		t.Errorf("Name() = %q, want %q", c.Name(), "elbv2")
	}
	if c.IsGlobal() {
		t.Error("IsGlobal() = true, want false — ELBv2 is a regional service")
	}
}

func TestMapLoadBalancer_NilState(t *testing.T) {
	c := NewCollector()
	// LoadBalancer.State is a pointer; the mapper must not panic when it is nil.
	lb := types.LoadBalancer{
		LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/no-state/abc"),
		LoadBalancerName: aws.String("no-state"),
		// State is nil
	}

	res := c.mapLoadBalancer("us-east-1", lb, services.DetailLevelSummary)

	if res.State != "" {
		t.Errorf("State = %q, want empty when State is nil", res.State)
	}
}

func TestMapLoadBalancer_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2024, 4, 5, 10, 0, 0, 0, time.UTC)
	lb := types.LoadBalancer{
		LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/abc123"),
		LoadBalancerName: aws.String("my-alb"),
		State:            &types.LoadBalancerState{Code: types.LoadBalancerStateEnumActive},
		Type:             types.LoadBalancerTypeEnumApplication,
		Scheme:           types.LoadBalancerSchemeEnumInternetFacing,
		DNSName:          aws.String("my-alb-123.us-east-1.elb.amazonaws.com"),
		VpcId:            aws.String("vpc-abc123"),
		IpAddressType:    types.IpAddressTypeIpv4,
		CreatedTime:      &created,
	}

	res := c.mapLoadBalancer("us-east-1", lb, services.DetailLevelSummary)

	if res.Service != "elbv2" {
		t.Errorf("Service = %q, want %q", res.Service, "elbv2")
	}
	if res.Type != "load-balancer" {
		t.Errorf("Type = %q, want %q", res.Type, "load-balancer")
	}
	if res.ID != "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/my-alb/abc123" {
		t.Errorf("ID = %q", res.ID)
	}
	if res.ARN != res.ID {
		t.Errorf("ARN != ID: ARN = %q", res.ARN)
	}
	if res.Name != "my-alb" {
		t.Errorf("Name = %q, want %q", res.Name, "my-alb")
	}
	if res.State != "active" {
		t.Errorf("State = %q, want %q", res.State, "active")
	}
	if res.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", res.Region, "us-east-1")
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapLoadBalancer_SummaryFields(t *testing.T) {
	c := NewCollector()
	lb := types.LoadBalancer{
		LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-west-2:123:loadbalancer/net/my-nlb/xyz"),
		LoadBalancerName: aws.String("my-nlb"),
		State:            &types.LoadBalancerState{Code: types.LoadBalancerStateEnumProvisioning},
		Type:             types.LoadBalancerTypeEnumNetwork,
		Scheme:           types.LoadBalancerSchemeEnumInternal,
		DNSName:          aws.String("my-nlb.elb.us-west-2.amazonaws.com"),
		VpcId:            aws.String("vpc-xyz"),
		IpAddressType:    types.IpAddressTypeDualstack,
	}

	res := c.mapLoadBalancer("us-west-2", lb, services.DetailLevelSummary)

	if res.Summary["type"] != "network" {
		t.Errorf("Summary[type] = %q, want %q", res.Summary["type"], "network")
	}
	if res.Summary["scheme"] != "internal" {
		t.Errorf("Summary[scheme] = %q, want %q", res.Summary["scheme"], "internal")
	}
	if res.Summary["dnsName"] != "my-nlb.elb.us-west-2.amazonaws.com" {
		t.Errorf("Summary[dnsName] = %q", res.Summary["dnsName"])
	}
	if res.Summary["vpcId"] != "vpc-xyz" {
		t.Errorf("Summary[vpcId] = %q", res.Summary["vpcId"])
	}
	if res.Summary["ipType"] != "dualstack" {
		t.Errorf("Summary[ipType] = %q, want %q", res.Summary["ipType"], "dualstack")
	}
}

func TestMapLoadBalancer_NilCreatedTime(t *testing.T) {
	c := NewCollector()
	lb := types.LoadBalancer{
		LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/no-time/000"),
		LoadBalancerName: aws.String("no-time"),
		State:            &types.LoadBalancerState{Code: types.LoadBalancerStateEnumActive},
	}

	res := c.mapLoadBalancer("us-east-1", lb, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt, got %v", res.CreatedAt)
	}
}

// fakeTagsAPI fails the first DescribeTags call and serves tags on later calls,
// so the per-batch continue-on-error behavior can be verified.
type fakeTagsAPI struct {
	calls   int
	failIdx int // 0-based call index that returns an error
	tags    map[string]map[string]string
}

func (f *fakeTagsAPI) DescribeTags(_ context.Context, in *elasticloadbalancingv2.DescribeTagsInput, _ ...func(*elasticloadbalancingv2.Options)) (*elasticloadbalancingv2.DescribeTagsOutput, error) {
	call := f.calls
	f.calls++
	if call == f.failIdx {
		return nil, errors.New("Throttling: rate exceeded")
	}
	var tds []types.TagDescription
	for _, arn := range in.ResourceArns {
		td := types.TagDescription{ResourceArn: aws.String(arn)}
		for k, v := range f.tags[arn] {
			td.Tags = append(td.Tags, types.Tag{Key: aws.String(k), Value: aws.String(v)})
		}
		tds = append(tds, td)
	}
	return &elasticloadbalancingv2.DescribeTagsOutput{TagDescriptions: tds}, nil
}

func TestApplyTags_ContinuesAfterBatchError(t *testing.T) {
	c := NewCollector()

	// Two full batches: the first batch's DescribeTags fails, the second succeeds.
	batch := make([]model.Resource, 0, describeTagsBatch+1)
	for i := 0; i < describeTagsBatch+1; i++ {
		arn := fmt.Sprintf("arn:aws:elasticloadbalancing:us-east-1:1:loadbalancer/app/lb%d", i)
		batch = append(batch, model.Resource{ARN: arn, Tags: map[string]string{}})
	}
	lastARN := batch[len(batch)-1].ARN

	api := &fakeTagsAPI{failIdx: 0, tags: map[string]map[string]string{
		lastARN: {"env": "prod"},
	}}

	c.applyTags(context.Background(), api, batch)

	if api.calls != 2 {
		t.Fatalf("expected both batches to be attempted, got %d calls", api.calls)
	}
	if batch[len(batch)-1].Tags["env"] != "prod" {
		t.Errorf("second-batch LB should be tagged despite the first batch failing, got %v", batch[len(batch)-1].Tags)
	}
}
