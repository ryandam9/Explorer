package vpctui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/smithy-go"

	"github.com/user/aws_explorer/internal/auth"
	"github.com/user/aws_explorer/internal/config"
)

const awsRequestTimeout = 30 * time.Second

// VPCClient wraps EC2, ELBv2, Lambda, and RDS clients for a single region.
type VPCClient struct {
	ec2    *awsec2.Client
	elbv2  *elbv2.Client
	lambda *lambda.Client
	rds    *rds.Client
	ctx    context.Context
}

func NewVPCClient(ctx context.Context, awsCfg *config.AWSConfig, region string) (*VPCClient, error) {
	cfg, err := auth.BuildAWSConfig(ctx, awsCfg, region)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %w", err)
	}
	return &VPCClient{
		ec2:    awsec2.NewFromConfig(cfg),
		elbv2:  elbv2.NewFromConfig(cfg),
		lambda: lambda.NewFromConfig(cfg),
		rds:    rds.NewFromConfig(cfg),
		ctx:    ctx,
	}, nil
}

func (c *VPCClient) requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.ctx, awsRequestTimeout)
}

func hasAPIErrorCode(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.ErrorCode() == code {
			return true
		}
	}
	return false
}

var fallbackRegions = []string{
	"af-south-1",
	"ap-east-1", "ap-northeast-1", "ap-northeast-2", "ap-northeast-3",
	"ap-south-1", "ap-south-2",
	"ap-southeast-1", "ap-southeast-2", "ap-southeast-3", "ap-southeast-4",
	"ca-central-1", "ca-west-1",
	"eu-central-1", "eu-central-2",
	"eu-north-1", "eu-south-1", "eu-south-2",
	"eu-west-1", "eu-west-2", "eu-west-3",
	"il-central-1",
	"me-central-1", "me-south-1",
	"mx-central-1",
	"sa-east-1",
	"us-east-1", "us-east-2", "us-west-1", "us-west-2",
}

func ListRegions(ctx context.Context, awsCfg *config.AWSConfig) []string {
	cfg, err := auth.BuildAWSConfig(ctx, awsCfg, "")
	if err != nil {
		return fallbackRegions
	}
	client := awsec2.NewFromConfig(cfg)
	output, err := client.DescribeRegions(ctx, &awsec2.DescribeRegionsInput{})
	if err != nil {
		return fallbackRegions
	}
	regions := make([]string, 0, len(output.Regions))
	for _, r := range output.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	sort.Strings(regions)
	if len(regions) == 0 {
		return fallbackRegions
	}
	return regions
}

func ListVPCsInRegion(ctx context.Context, awsCfg *config.AWSConfig, region string) ([]VPCInfo, error) {
	client, err := NewVPCClient(ctx, awsCfg, region)
	if err != nil {
		return nil, err
	}
	vpcs, err := client.ListVPCs()
	if err != nil {
		if hasAPIErrorCode(err, "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "AuthorizationError") {
			return nil, nil
		}
		return nil, err
	}
	for i := range vpcs {
		vpcs[i].Region = region
	}
	return vpcs, nil
}

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type VPCInfo struct {
	ID              string
	Name            string
	Region          string
	CIDR            string
	State           string
	IsDefault       bool
	DhcpOptionsID   string
	InstanceTenancy string
	OwnerId         string
	Ipv6CIDRs       []string
	Tags            map[string]string
}

type SubnetInfo struct {
	ID                  string
	Name                string
	VPCID               string
	CIDR                string
	AZ                  string
	AvailableIPs        int32
	IsPublic            bool
	State               string
	DefaultForAz        bool
	MapPublicIPOnLaunch bool
	Ipv6CIDRs           []string
	Tags                map[string]string
}

type SGRule struct {
	Direction   string
	Protocol    string
	PortRange   string
	Source      string
	Description string
}

type SGInfo struct {
	ID            string
	Name          string
	Description   string
	VPCID         string
	InboundCount  int
	OutboundCount int
	Rules         []SGRule
	Tags          map[string]string
}

type Route struct {
	Destination string
	Target      string
	State       string
}

type RouteTableInfo struct {
	ID           string
	Name         string
	VPCID        string
	IsMain       bool
	Routes       []Route
	Associations []string
	Tags         map[string]string
}

type IGWInfo struct {
	ID    string
	Name  string
	State string
	VPCID string
	Tags  map[string]string
}

type NatGWInfo struct {
	ID        string
	Name      string
	State     string
	SubnetID  string
	VPCID     string
	Type      string
	PublicIP  string
	PrivateIP string
	Tags      map[string]string
}

type EndpointInfo struct {
	ID          string
	ServiceName string
	Type        string
	State       string
	VPCID       string
	Tags        map[string]string
}

type NACLRule struct {
	RuleNumber int32
	Protocol   string
	PortRange  string
	CIDR       string
	Action     string
	Direction  string
}

type NACLInfo struct {
	ID           string
	Name         string
	VPCID        string
	IsDefault    bool
	Associations []string
	Rules        []NACLRule
	Tags         map[string]string
}

type PeeringInfo struct {
	ID              string
	Status          string
	RequesterVPCID  string
	RequesterRegion string
	RequesterCIDR   string
	AccepterVPCID   string
	AccepterRegion  string
	AccepterCIDR    string
	Tags            map[string]string
}

type FlowLogInfo struct {
	ID             string
	ResourceID     string
	LogDestination string
	TrafficType    string
	Status         string
	LogFormat      string
	Tags           map[string]string
}

type EC2InstanceInfo struct {
	ID         string
	Name       string
	State      string
	Type       string
	PrivateIP  string
	PublicIP   string
	VPCID      string
	SubnetID   string
	AZ         string
	Platform   string
	LaunchTime string
	Tags       map[string]string
}

type LambdaFunctionInfo struct {
	Name         string
	Runtime      string
	State        string
	VPCID        string
	SubnetIDs    []string
	SGIDs        []string
	Handler      string
	MemoryMB     int32
	TimeoutSec   int32
	LastModified string
}

type RDSInstanceInfo struct {
	ID       string
	Endpoint string
	Engine   string
	Class    string
	Status   string
	VPCID    string
	AZ       string
	MultiAZ  bool
	Storage  int32
}

type LoadBalancerInfo struct {
	Name      string
	ARN       string
	Type      string
	Scheme    string
	State     string
	VPCID     string
	DNSName   string
	CreatedAt string
}

// ---------------------------------------------------------------------------
// API methods
// ---------------------------------------------------------------------------

func (c *VPCClient) ListVPCs() ([]VPCInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var vpcs []VPCInfo
	paginator := awsec2.NewDescribeVpcsPaginator(c.ec2, &awsec2.DescribeVpcsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, v := range page.Vpcs {
			info := VPCInfo{
				ID:              aws.ToString(v.VpcId),
				Name:            ec2TagName(v.Tags),
				CIDR:            aws.ToString(v.CidrBlock),
				State:           string(v.State),
				IsDefault:       aws.ToBool(v.IsDefault),
				DhcpOptionsID:   aws.ToString(v.DhcpOptionsId),
				InstanceTenancy: string(v.InstanceTenancy),
				OwnerId:         aws.ToString(v.OwnerId),
				Tags:            ec2TagsToMap(v.Tags),
			}
			for _, assoc := range v.Ipv6CidrBlockAssociationSet {
				info.Ipv6CIDRs = append(info.Ipv6CIDRs, aws.ToString(assoc.Ipv6CidrBlock))
			}
			vpcs = append(vpcs, info)
		}
	}
	return vpcs, nil
}

func (c *VPCClient) ListSubnets(vpcID string) ([]SubnetInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeSubnetsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var subnets []SubnetInfo
	paginator := awsec2.NewDescribeSubnetsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range page.Subnets {
			info := SubnetInfo{
				ID:                  aws.ToString(s.SubnetId),
				Name:                ec2TagName(s.Tags),
				VPCID:               aws.ToString(s.VpcId),
				CIDR:                aws.ToString(s.CidrBlock),
				AZ:                  aws.ToString(s.AvailabilityZone),
				AvailableIPs:        aws.ToInt32(s.AvailableIpAddressCount),
				IsPublic:            aws.ToBool(s.MapPublicIpOnLaunch),
				State:               string(s.State),
				DefaultForAz:        aws.ToBool(s.DefaultForAz),
				MapPublicIPOnLaunch: aws.ToBool(s.MapPublicIpOnLaunch),
				Tags:                ec2TagsToMap(s.Tags),
			}
			for _, assoc := range s.Ipv6CidrBlockAssociationSet {
				info.Ipv6CIDRs = append(info.Ipv6CIDRs, aws.ToString(assoc.Ipv6CidrBlock))
			}
			subnets = append(subnets, info)
		}
	}
	return subnets, nil
}

func (c *VPCClient) ListSecurityGroups(vpcID string) ([]SGInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var sgs []SGInfo
	paginator := awsec2.NewDescribeSecurityGroupsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, sg := range page.SecurityGroups {
			info := SGInfo{
				ID:          aws.ToString(sg.GroupId),
				Name:        aws.ToString(sg.GroupName),
				Description: aws.ToString(sg.Description),
				VPCID:       aws.ToString(sg.VpcId),
				Tags:        ec2TagsToMap(sg.Tags),
			}
			for _, perm := range sg.IpPermissions {
				info.Rules = append(info.Rules, permToRules(perm, "Inbound")...)
				info.InboundCount++
			}
			for _, perm := range sg.IpPermissionsEgress {
				info.Rules = append(info.Rules, permToRules(perm, "Outbound")...)
				info.OutboundCount++
			}
			sgs = append(sgs, info)
		}
	}
	return sgs, nil
}

func (c *VPCClient) ListRouteTables(vpcID string) ([]RouteTableInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var tables []RouteTableInfo
	paginator := awsec2.NewDescribeRouteTablesPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, rt := range page.RouteTables {
			info := RouteTableInfo{
				ID:    aws.ToString(rt.RouteTableId),
				Name:  ec2TagName(rt.Tags),
				VPCID: aws.ToString(rt.VpcId),
				Tags:  ec2TagsToMap(rt.Tags),
			}
			for _, assoc := range rt.Associations {
				if aws.ToBool(assoc.Main) {
					info.IsMain = true
				}
				if assoc.SubnetId != nil {
					info.Associations = append(info.Associations, *assoc.SubnetId)
				}
			}
			for _, r := range rt.Routes {
				dest := aws.ToString(r.DestinationCidrBlock)
				if dest == "" {
					dest = aws.ToString(r.DestinationIpv6CidrBlock)
				}
				if dest == "" {
					dest = aws.ToString(r.DestinationPrefixListId)
				}
				info.Routes = append(info.Routes, Route{
					Destination: dest,
					Target:      routeTarget(r),
					State:       string(r.State),
				})
			}
			tables = append(tables, info)
		}
	}
	return tables, nil
}

func (c *VPCClient) ListInternetGateways(vpcID string) ([]IGWInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeInternetGatewaysInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}},
		},
	}
	var igws []IGWInfo
	paginator := awsec2.NewDescribeInternetGatewaysPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, igw := range page.InternetGateways {
			state := "detached"
			for _, att := range igw.Attachments {
				if aws.ToString(att.VpcId) == vpcID {
					state = string(att.State)
				}
			}
			igws = append(igws, IGWInfo{
				ID:    aws.ToString(igw.InternetGatewayId),
				Name:  ec2TagName(igw.Tags),
				State: state,
				VPCID: vpcID,
				Tags:  ec2TagsToMap(igw.Tags),
			})
		}
	}
	return igws, nil
}

func (c *VPCClient) ListNatGateways(vpcID string) ([]NatGWInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeNatGatewaysInput{
		Filter: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var gateways []NatGWInfo
	paginator := awsec2.NewDescribeNatGatewaysPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, ngw := range page.NatGateways {
			info := NatGWInfo{
				ID:       aws.ToString(ngw.NatGatewayId),
				Name:     ec2TagName(ngw.Tags),
				State:    string(ngw.State),
				SubnetID: aws.ToString(ngw.SubnetId),
				VPCID:    aws.ToString(ngw.VpcId),
				Type:     string(ngw.ConnectivityType),
				Tags:     ec2TagsToMap(ngw.Tags),
			}
			for _, addr := range ngw.NatGatewayAddresses {
				if addr.PublicIp != nil {
					info.PublicIP = *addr.PublicIp
				}
				if addr.PrivateIp != nil {
					info.PrivateIP = *addr.PrivateIp
				}
			}
			gateways = append(gateways, info)
		}
	}
	return gateways, nil
}

func (c *VPCClient) ListVPCEndpoints(vpcID string) ([]EndpointInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeVpcEndpointsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var endpoints []EndpointInfo
	paginator := awsec2.NewDescribeVpcEndpointsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, ep := range page.VpcEndpoints {
			endpoints = append(endpoints, EndpointInfo{
				ID:          aws.ToString(ep.VpcEndpointId),
				ServiceName: aws.ToString(ep.ServiceName),
				Type:        string(ep.VpcEndpointType),
				State:       string(ep.State),
				VPCID:       aws.ToString(ep.VpcId),
				Tags:        ec2TagsToMap(ep.Tags),
			})
		}
	}
	return endpoints, nil
}

func (c *VPCClient) ListNetworkACLs(vpcID string) ([]NACLInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeNetworkAclsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var nacls []NACLInfo
	paginator := awsec2.NewDescribeNetworkAclsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, nacl := range page.NetworkAcls {
			info := NACLInfo{
				ID:        aws.ToString(nacl.NetworkAclId),
				Name:      ec2TagName(nacl.Tags),
				VPCID:     aws.ToString(nacl.VpcId),
				IsDefault: aws.ToBool(nacl.IsDefault),
				Tags:      ec2TagsToMap(nacl.Tags),
			}
			for _, assoc := range nacl.Associations {
				if assoc.SubnetId != nil {
					info.Associations = append(info.Associations, *assoc.SubnetId)
				}
			}
			for _, entry := range nacl.Entries {
				dir := "Inbound"
				if aws.ToBool(entry.Egress) {
					dir = "Outbound"
				}
				cidr := aws.ToString(entry.CidrBlock)
				if cidr == "" {
					cidr = aws.ToString(entry.Ipv6CidrBlock)
				}
				portRange := "All"
				if entry.PortRange != nil {
					from := aws.ToInt32(entry.PortRange.From)
					to := aws.ToInt32(entry.PortRange.To)
					if from == to {
						portRange = fmt.Sprintf("%d", from)
					} else {
						portRange = fmt.Sprintf("%d-%d", from, to)
					}
				}
				info.Rules = append(info.Rules, NACLRule{
					RuleNumber: aws.ToInt32(entry.RuleNumber),
					Protocol:   naclProtocol(aws.ToString(entry.Protocol)),
					PortRange:  portRange,
					CIDR:       cidr,
					Action:     string(entry.RuleAction),
					Direction:  dir,
				})
			}
			nacls = append(nacls, info)
		}
	}
	return nacls, nil
}

func (c *VPCClient) ListPeeringConnections(vpcID string) ([]PeeringInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	seen := map[string]bool{}
	var all []PeeringInfo

	for _, filterName := range []string{"requester-vpc-info.vpc-id", "accepter-vpc-info.vpc-id"} {
		input := &awsec2.DescribeVpcPeeringConnectionsInput{
			Filters: []ec2types.Filter{
				{Name: aws.String(filterName), Values: []string{vpcID}},
			},
		}
		paginator := awsec2.NewDescribeVpcPeeringConnectionsPaginator(c.ec2, input)
		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, pc := range page.VpcPeeringConnections {
				id := aws.ToString(pc.VpcPeeringConnectionId)
				if seen[id] {
					continue
				}
				seen[id] = true
				info := PeeringInfo{
					ID:   id,
					Tags: ec2TagsToMap(pc.Tags),
				}
				if pc.Status != nil {
					info.Status = string(pc.Status.Code)
				}
				if pc.RequesterVpcInfo != nil {
					info.RequesterVPCID = aws.ToString(pc.RequesterVpcInfo.VpcId)
					info.RequesterCIDR = aws.ToString(pc.RequesterVpcInfo.CidrBlock)
					info.RequesterRegion = aws.ToString(pc.RequesterVpcInfo.Region)
				}
				if pc.AccepterVpcInfo != nil {
					info.AccepterVPCID = aws.ToString(pc.AccepterVpcInfo.VpcId)
					info.AccepterCIDR = aws.ToString(pc.AccepterVpcInfo.CidrBlock)
					info.AccepterRegion = aws.ToString(pc.AccepterVpcInfo.Region)
				}
				all = append(all, info)
			}
		}
	}
	return all, nil
}

func (c *VPCClient) ListFlowLogs(vpcID string) ([]FlowLogInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeFlowLogsInput{
		Filter: []ec2types.Filter{
			{Name: aws.String("resource-id"), Values: []string{vpcID}},
		},
	}
	var logs []FlowLogInfo
	paginator := awsec2.NewDescribeFlowLogsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, fl := range page.FlowLogs {
			logs = append(logs, FlowLogInfo{
				ID:             aws.ToString(fl.FlowLogId),
				ResourceID:     aws.ToString(fl.ResourceId),
				LogDestination: aws.ToString(fl.LogDestination),
				TrafficType:    string(fl.TrafficType),
				Status:         aws.ToString(fl.FlowLogStatus),
				LogFormat:      aws.ToString(fl.LogFormat),
				Tags:           ec2TagsToMap(fl.Tags),
			})
		}
	}
	return logs, nil
}

func (c *VPCClient) ListEC2Instances(vpcID string) ([]EC2InstanceInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpc-id"), Values: []string{vpcID}},
		},
	}
	var instances []EC2InstanceInfo
	paginator := awsec2.NewDescribeInstancesPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				launchTime := ""
				if inst.LaunchTime != nil {
					launchTime = inst.LaunchTime.Format("2006-01-02 15:04")
				}
				platform := "linux"
				if inst.Platform != "" {
					platform = string(inst.Platform)
				}
				az := ""
				if inst.Placement != nil {
					az = aws.ToString(inst.Placement.AvailabilityZone)
				}
				instances = append(instances, EC2InstanceInfo{
					ID:         aws.ToString(inst.InstanceId),
					Name:       ec2TagName(inst.Tags),
					State:      string(inst.State.Name),
					Type:       string(inst.InstanceType),
					PrivateIP:  aws.ToString(inst.PrivateIpAddress),
					PublicIP:   aws.ToString(inst.PublicIpAddress),
					VPCID:      aws.ToString(inst.VpcId),
					SubnetID:   aws.ToString(inst.SubnetId),
					AZ:         az,
					Platform:   platform,
					LaunchTime: launchTime,
					Tags:       ec2TagsToMap(inst.Tags),
				})
			}
		}
	}
	return instances, nil
}

func (c *VPCClient) ListLambdaFunctions(vpcID string) ([]LambdaFunctionInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var functions []LambdaFunctionInfo
	var marker *string
	for {
		output, err := c.lambda.ListFunctions(ctx, &lambda.ListFunctionsInput{Marker: marker})
		if err != nil {
			if hasAPIErrorCode(err, "AccessDeniedException") {
				return nil, nil
			}
			return nil, err
		}
		for _, fn := range output.Functions {
			if fn.VpcConfig == nil || aws.ToString(fn.VpcConfig.VpcId) != vpcID {
				continue
			}
			state := "active"
			if fn.State != "" {
				state = string(fn.State)
			}
			functions = append(functions, LambdaFunctionInfo{
				Name:         aws.ToString(fn.FunctionName),
				Runtime:      string(fn.Runtime),
				State:        state,
				VPCID:        aws.ToString(fn.VpcConfig.VpcId),
				SubnetIDs:    fn.VpcConfig.SubnetIds,
				SGIDs:        fn.VpcConfig.SecurityGroupIds,
				Handler:      aws.ToString(fn.Handler),
				MemoryMB:     aws.ToInt32(fn.MemorySize),
				TimeoutSec:   aws.ToInt32(fn.Timeout),
				LastModified: aws.ToString(fn.LastModified),
			})
		}
		if output.NextMarker == nil {
			break
		}
		marker = output.NextMarker
	}
	return functions, nil
}

func (c *VPCClient) ListRDSInstances(vpcID string) ([]RDSInstanceInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var instances []RDSInstanceInfo
	var marker *string
	for {
		output, err := c.rds.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{Marker: marker})
		if err != nil {
			if hasAPIErrorCode(err, "AccessDenied", "AccessDeniedException") {
				return nil, nil
			}
			return nil, err
		}
		for _, db := range output.DBInstances {
			if db.DBSubnetGroup == nil || aws.ToString(db.DBSubnetGroup.VpcId) != vpcID {
				continue
			}
			endpoint := ""
			if db.Endpoint != nil {
				endpoint = fmt.Sprintf("%s:%d", aws.ToString(db.Endpoint.Address), aws.ToInt32(db.Endpoint.Port))
			}
			instances = append(instances, RDSInstanceInfo{
				ID:       aws.ToString(db.DBInstanceIdentifier),
				Endpoint: endpoint,
				Engine:   fmt.Sprintf("%s %s", aws.ToString(db.Engine), aws.ToString(db.EngineVersion)),
				Class:    aws.ToString(db.DBInstanceClass),
				Status:   aws.ToString(db.DBInstanceStatus),
				VPCID:    aws.ToString(db.DBSubnetGroup.VpcId),
				AZ:       aws.ToString(db.AvailabilityZone),
				MultiAZ:  aws.ToBool(db.MultiAZ),
				Storage:  aws.ToInt32(db.AllocatedStorage),
			})
		}
		if output.Marker == nil {
			break
		}
		marker = output.Marker
	}
	return instances, nil
}

func (c *VPCClient) ListLoadBalancers(vpcID string) ([]LoadBalancerInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var lbs []LoadBalancerInfo
	var marker *string
	for {
		output, err := c.elbv2.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{Marker: marker})
		if err != nil {
			if hasAPIErrorCode(err, "AccessDenied", "AccessDeniedException") {
				return nil, nil
			}
			return nil, err
		}
		for _, lb := range output.LoadBalancers {
			if aws.ToString(lb.VpcId) != vpcID {
				continue
			}
			state := "unknown"
			if lb.State != nil {
				state = string(lb.State.Code)
			}
			created := ""
			if lb.CreatedTime != nil {
				created = lb.CreatedTime.Format("2006-01-02")
			}
			lbs = append(lbs, LoadBalancerInfo{
				Name:      aws.ToString(lb.LoadBalancerName),
				ARN:       aws.ToString(lb.LoadBalancerArn),
				Type:      string(lb.Type),
				Scheme:    string(lb.Scheme),
				State:     state,
				VPCID:     aws.ToString(lb.VpcId),
				DNSName:   aws.ToString(lb.DNSName),
				CreatedAt: created,
			})
		}
		if output.NextMarker == nil {
			break
		}
		marker = output.NextMarker
	}
	return lbs, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func ec2TagName(tags []ec2types.Tag) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == "Name" {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func ec2TagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		m[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return m
}

func routeTarget(r ec2types.Route) string {
	switch {
	case r.GatewayId != nil && *r.GatewayId != "":
		return *r.GatewayId
	case r.NatGatewayId != nil:
		return *r.NatGatewayId
	case r.TransitGatewayId != nil:
		return *r.TransitGatewayId
	case r.VpcPeeringConnectionId != nil:
		return *r.VpcPeeringConnectionId
	case r.NetworkInterfaceId != nil:
		return *r.NetworkInterfaceId
	case r.InstanceId != nil:
		return *r.InstanceId
	case r.EgressOnlyInternetGatewayId != nil:
		return *r.EgressOnlyInternetGatewayId
	default:
		return "-"
	}
}

func permToRules(perm ec2types.IpPermission, dir string) []SGRule {
	proto := sgProtocol(aws.ToString(perm.IpProtocol))
	ports := sgPortRange(perm.FromPort, perm.ToPort)
	var rules []SGRule
	for _, r := range perm.IpRanges {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, PortRange: ports, Source: aws.ToString(r.CidrIp), Description: aws.ToString(r.Description)})
	}
	for _, r := range perm.Ipv6Ranges {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, PortRange: ports, Source: aws.ToString(r.CidrIpv6), Description: aws.ToString(r.Description)})
	}
	for _, g := range perm.UserIdGroupPairs {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, PortRange: ports, Source: aws.ToString(g.GroupId)})
	}
	if len(rules) == 0 {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, PortRange: ports, Source: "-"})
	}
	return rules
}

func sgProtocol(proto string) string {
	switch proto {
	case "-1":
		return "All"
	case "tcp":
		return "TCP"
	case "udp":
		return "UDP"
	case "icmp":
		return "ICMP"
	case "icmpv6":
		return "ICMPv6"
	default:
		return strings.ToUpper(proto)
	}
}

func sgPortRange(from, to *int32) string {
	if from == nil {
		return "All"
	}
	f := aws.ToInt32(from)
	t := aws.ToInt32(to)
	if f == -1 {
		return "All"
	}
	if f == t {
		return fmt.Sprintf("%d", f)
	}
	return fmt.Sprintf("%d-%d", f, t)
}

func naclProtocol(proto string) string {
	switch proto {
	case "-1":
		return "All"
	case "6":
		return "TCP"
	case "17":
		return "UDP"
	case "1":
		return "ICMP"
	default:
		return proto
	}
}
