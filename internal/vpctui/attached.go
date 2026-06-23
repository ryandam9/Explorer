package vpctui

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
)

// ---------------------------------------------------------------------------
// Additional resources attached to a VPC
//
// These power the exhaustive Markdown export: every workload service that runs
// inside the VPC (ECS, EKS, ElastiCache, Redshift, EFS, EMR) plus the EC2-native
// VPN / transit-gateway plumbing that connects it to other networks. Each
// collector is best-effort: a missing-permission error is swallowed (returns an
// empty slice) so one denied API never aborts the whole report.
// ---------------------------------------------------------------------------

// accessDenied reports whether err is one of the assorted access-denied codes
// the various services return, so a collector can degrade to an empty result.
func accessDenied(err error) bool {
	return hasAPIErrorCode(err,
		"AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "AuthorizationError")
}

// ---------------------------------------------------------------------------
// Data types
// ---------------------------------------------------------------------------

type VPNGatewayInfo struct {
	ID            string
	State         string
	Type          string
	AmazonSideASN string
	Tags          map[string]string
}

type VPNConnectionInfo struct {
	ID                string
	State             string
	Type              string
	Category          string
	CustomerGatewayID string
	VPNGatewayID      string
	TransitGatewayID  string
	Tags              map[string]string
}

type CustomerGatewayInfo struct {
	ID        string
	State     string
	Type      string
	IPAddress string
	BgpAsn    string
	Tags      map[string]string
}

type TransitGatewayAttachmentInfo struct {
	ID               string
	TransitGatewayID string
	State            string
	ResourceType     string
	ResourceID       string
	Tags             map[string]string
}

type ECSServiceInfo struct {
	Cluster      string
	Name         string
	Status       string
	LaunchType   string
	DesiredCount int32
	RunningCount int32
	SubnetIDs    []string
	SGIDs        []string
}

type EKSClusterInfo struct {
	Name           string
	Status         string
	Version        string
	Endpoint       string
	VPCID          string
	SubnetIDs      []string
	SecurityGroups []string
}

type ElastiCacheClusterInfo struct {
	ID            string
	Engine        string
	EngineVersion string
	Status        string
	NodeType      string
	NumNodes      int32
	SubnetGroup   string
	VPCID         string
}

type RedshiftClusterInfo struct {
	ID          string
	Status      string
	NodeType    string
	NumNodes    int32
	DBName      string
	Endpoint    string
	SubnetGroup string
	VPCID       string
}

type EFSFileSystemInfo struct {
	ID              string
	Name            string
	State           string
	PerformanceMode string
	Encrypted       bool
	MountTargets    int // mount targets that land in this VPC
	SubnetIDs       []string
	VPCID           string
}

type EMRClusterInfo struct {
	ID       string
	Name     string
	State    string
	SubnetID string
}

// ---------------------------------------------------------------------------
// EC2-native: VPN gateways, VPN connections, customer gateways, TGW attachments
// ---------------------------------------------------------------------------

func (c *VPCClient) ListVPNGateways(vpcID string) ([]VPNGatewayInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.ec2.DescribeVpnGateways(ctx, &awsec2.DescribeVpnGatewaysInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("attachment.vpc-id"), Values: []string{vpcID}},
		},
	})
	if err != nil {
		if accessDenied(err) {
			return nil, nil
		}
		return nil, err
	}
	var gws []VPNGatewayInfo
	for _, g := range out.VpnGateways {
		state := string(g.State)
		for _, att := range g.VpcAttachments {
			if aws.ToString(att.VpcId) == vpcID {
				state = string(att.State)
			}
		}
		asn := ""
		if g.AmazonSideAsn != nil {
			asn = fmt.Sprintf("%d", aws.ToInt64(g.AmazonSideAsn))
		}
		gws = append(gws, VPNGatewayInfo{
			ID:            aws.ToString(g.VpnGatewayId),
			State:         state,
			Type:          string(g.Type),
			AmazonSideASN: asn,
			Tags:          ec2TagsToMap(g.Tags),
		})
	}
	return gws, nil
}

// ListVPNConnections returns the site-to-site VPN connections terminating on
// any of the supplied virtual private gateways (those attached to the VPC).
func (c *VPCClient) ListVPNConnections(vgwIDs []string) ([]VPNConnectionInfo, error) {
	if len(vgwIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.ec2.DescribeVpnConnections(ctx, &awsec2.DescribeVpnConnectionsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("vpn-gateway-id"), Values: vgwIDs},
		},
	})
	if err != nil {
		if accessDenied(err) {
			return nil, nil
		}
		return nil, err
	}
	var conns []VPNConnectionInfo
	for _, v := range out.VpnConnections {
		conns = append(conns, VPNConnectionInfo{
			ID:                aws.ToString(v.VpnConnectionId),
			State:             string(v.State),
			Type:              string(v.Type),
			Category:          aws.ToString(v.Category),
			CustomerGatewayID: aws.ToString(v.CustomerGatewayId),
			VPNGatewayID:      aws.ToString(v.VpnGatewayId),
			TransitGatewayID:  aws.ToString(v.TransitGatewayId),
			Tags:              ec2TagsToMap(v.Tags),
		})
	}
	return conns, nil
}

// ListCustomerGateways resolves the on-premises customer gateways referenced by
// the VPC's VPN connections.
func (c *VPCClient) ListCustomerGateways(cgwIDs []string) ([]CustomerGatewayInfo, error) {
	if len(cgwIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := c.requestContext()
	defer cancel()

	out, err := c.ec2.DescribeCustomerGateways(ctx, &awsec2.DescribeCustomerGatewaysInput{
		CustomerGatewayIds: cgwIDs,
	})
	if err != nil {
		if accessDenied(err) {
			return nil, nil
		}
		return nil, err
	}
	var gws []CustomerGatewayInfo
	for _, g := range out.CustomerGateways {
		gws = append(gws, CustomerGatewayInfo{
			ID:        aws.ToString(g.CustomerGatewayId),
			State:     aws.ToString(g.State),
			Type:      aws.ToString(g.Type),
			IPAddress: aws.ToString(g.IpAddress),
			BgpAsn:    aws.ToString(g.BgpAsn),
			Tags:      ec2TagsToMap(g.Tags),
		})
	}
	return gws, nil
}

func (c *VPCClient) ListTransitGatewayAttachments(vpcID string) ([]TransitGatewayAttachmentInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	input := &awsec2.DescribeTransitGatewayAttachmentsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("resource-id"), Values: []string{vpcID}},
			{Name: aws.String("resource-type"), Values: []string{"vpc"}},
		},
	}
	var atts []TransitGatewayAttachmentInfo
	paginator := awsec2.NewDescribeTransitGatewayAttachmentsPaginator(c.ec2, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, a := range page.TransitGatewayAttachments {
			atts = append(atts, TransitGatewayAttachmentInfo{
				ID:               aws.ToString(a.TransitGatewayAttachmentId),
				TransitGatewayID: aws.ToString(a.TransitGatewayId),
				State:            string(a.State),
				ResourceType:     string(a.ResourceType),
				ResourceID:       aws.ToString(a.ResourceId),
				Tags:             ec2TagsToMap(a.Tags),
			})
		}
	}
	return atts, nil
}

// ---------------------------------------------------------------------------
// Workload services
// ---------------------------------------------------------------------------

// ListECSServices returns the ECS services whose awsvpc network configuration
// places tasks in one of the VPC's subnets. EC2-launch-type services without an
// awsvpc configuration cannot be attributed to a VPC and are skipped.
func (c *VPCClient) ListECSServices(vpcSubnets map[string]bool) ([]ECSServiceInfo, error) {
	if len(vpcSubnets) == 0 {
		return nil, nil
	}
	ctx, cancel := c.requestContext()
	defer cancel()

	var out []ECSServiceInfo
	clusterPaginator := ecs.NewListClustersPaginator(c.ecs, &ecs.ListClustersInput{})
	for clusterPaginator.HasMorePages() {
		cpage, err := clusterPaginator.NextPage(ctx)
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, clusterArn := range cpage.ClusterArns {
			svcPaginator := ecs.NewListServicesPaginator(c.ecs, &ecs.ListServicesInput{
				Cluster: aws.String(clusterArn),
			})
			for svcPaginator.HasMorePages() {
				spage, err := svcPaginator.NextPage(ctx)
				if err != nil {
					break
				}
				for i := 0; i < len(spage.ServiceArns); i += 10 {
					end := i + 10
					if end > len(spage.ServiceArns) {
						end = len(spage.ServiceArns)
					}
					desc, err := c.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
						Cluster:  aws.String(clusterArn),
						Services: spage.ServiceArns[i:end],
					})
					if err != nil {
						continue
					}
					for _, svc := range desc.Services {
						var subnets, sgs []string
						if svc.NetworkConfiguration != nil && svc.NetworkConfiguration.AwsvpcConfiguration != nil {
							subnets = svc.NetworkConfiguration.AwsvpcConfiguration.Subnets
							sgs = svc.NetworkConfiguration.AwsvpcConfiguration.SecurityGroups
						}
						if !anyInSet(subnets, vpcSubnets) {
							continue
						}
						out = append(out, ECSServiceInfo{
							Cluster:      shortName(clusterArn),
							Name:         aws.ToString(svc.ServiceName),
							Status:       aws.ToString(svc.Status),
							LaunchType:   string(svc.LaunchType),
							DesiredCount: svc.DesiredCount,
							RunningCount: svc.RunningCount,
							SubnetIDs:    subnets,
							SGIDs:        sgs,
						})
					}
				}
			}
		}
	}
	return out, nil
}

func (c *VPCClient) ListEKSClusters(vpcID string) ([]EKSClusterInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var clusters []EKSClusterInfo
	paginator := eks.NewListClustersPaginator(c.eks, &eks.ListClustersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, name := range page.Clusters {
			desc, err := c.eks.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(name)})
			if err != nil {
				continue
			}
			cl := desc.Cluster
			if cl == nil || cl.ResourcesVpcConfig == nil || aws.ToString(cl.ResourcesVpcConfig.VpcId) != vpcID {
				continue
			}
			clusters = append(clusters, EKSClusterInfo{
				Name:           aws.ToString(cl.Name),
				Status:         string(cl.Status),
				Version:        aws.ToString(cl.Version),
				Endpoint:       aws.ToString(cl.Endpoint),
				VPCID:          aws.ToString(cl.ResourcesVpcConfig.VpcId),
				SubnetIDs:      cl.ResourcesVpcConfig.SubnetIds,
				SecurityGroups: cl.ResourcesVpcConfig.SecurityGroupIds,
			})
		}
	}
	return clusters, nil
}

func (c *VPCClient) ListElastiCacheClusters(vpcID string) ([]ElastiCacheClusterInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	// Cache clusters reference a subnet group by name; the subnet group carries
	// the VPC ID. Build the name→VPC map first, then keep only the clusters
	// whose subnet group lives in this VPC.
	groupVPC := map[string]string{}
	var sgMarker *string
	for {
		out, err := c.elasticache.DescribeCacheSubnetGroups(ctx, &elasticache.DescribeCacheSubnetGroupsInput{Marker: sgMarker})
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, g := range out.CacheSubnetGroups {
			groupVPC[aws.ToString(g.CacheSubnetGroupName)] = aws.ToString(g.VpcId)
		}
		if out.Marker == nil {
			break
		}
		sgMarker = out.Marker
	}

	var clusters []ElastiCacheClusterInfo
	var marker *string
	for {
		out, err := c.elasticache.DescribeCacheClusters(ctx, &elasticache.DescribeCacheClustersInput{Marker: marker})
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, cl := range out.CacheClusters {
			grp := aws.ToString(cl.CacheSubnetGroupName)
			if grp == "" || groupVPC[grp] != vpcID {
				continue
			}
			clusters = append(clusters, ElastiCacheClusterInfo{
				ID:            aws.ToString(cl.CacheClusterId),
				Engine:        aws.ToString(cl.Engine),
				EngineVersion: aws.ToString(cl.EngineVersion),
				Status:        aws.ToString(cl.CacheClusterStatus),
				NodeType:      aws.ToString(cl.CacheNodeType),
				NumNodes:      aws.ToInt32(cl.NumCacheNodes),
				SubnetGroup:   grp,
				VPCID:         vpcID,
			})
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return clusters, nil
}

func (c *VPCClient) ListRedshiftClusters(vpcID string) ([]RedshiftClusterInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var clusters []RedshiftClusterInfo
	var marker *string
	for {
		out, err := c.redshift.DescribeClusters(ctx, &redshift.DescribeClustersInput{Marker: marker})
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, cl := range out.Clusters {
			if aws.ToString(cl.VpcId) != vpcID {
				continue
			}
			endpoint := ""
			if cl.Endpoint != nil {
				endpoint = fmt.Sprintf("%s:%d", aws.ToString(cl.Endpoint.Address), aws.ToInt32(cl.Endpoint.Port))
			}
			clusters = append(clusters, RedshiftClusterInfo{
				ID:          aws.ToString(cl.ClusterIdentifier),
				Status:      aws.ToString(cl.ClusterStatus),
				NodeType:    aws.ToString(cl.NodeType),
				NumNodes:    aws.ToInt32(cl.NumberOfNodes),
				DBName:      aws.ToString(cl.DBName),
				Endpoint:    endpoint,
				SubnetGroup: aws.ToString(cl.ClusterSubnetGroupName),
				VPCID:       aws.ToString(cl.VpcId),
			})
		}
		if out.Marker == nil {
			break
		}
		marker = out.Marker
	}
	return clusters, nil
}

func (c *VPCClient) ListEFSFileSystems(vpcID string) ([]EFSFileSystemInfo, error) {
	ctx, cancel := c.requestContext()
	defer cancel()

	var systems []EFSFileSystemInfo
	var marker *string
	for {
		out, err := c.efs.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{Marker: marker})
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, fs := range out.FileSystems {
			// A filesystem can have mount targets across many pages; reading only
			// the first page can undercount in-VPC targets or drop the filesystem
			// entirely (CLAUDE.md §5). Don't swallow the error silently either (§6a).
			var subnetIDs []string
			var mtMarker *string
			mtFailed := false
			for {
				mts, err := c.efs.DescribeMountTargets(ctx, &efs.DescribeMountTargetsInput{
					FileSystemId: fs.FileSystemId,
					Marker:       mtMarker,
				})
				if err != nil {
					slog.Warn("efs: describe mount targets failed", "filesystem", aws.ToString(fs.FileSystemId), "err", err)
					mtFailed = true
					break
				}
				for _, mt := range mts.MountTargets {
					if aws.ToString(mt.VpcId) == vpcID {
						subnetIDs = append(subnetIDs, aws.ToString(mt.SubnetId))
					}
				}
				if mts.NextMarker == nil {
					break
				}
				mtMarker = mts.NextMarker
			}
			if mtFailed || len(subnetIDs) == 0 {
				continue
			}
			name := aws.ToString(fs.Name)
			if name == "" {
				name = aws.ToString(fs.FileSystemId)
			}
			systems = append(systems, EFSFileSystemInfo{
				ID:              aws.ToString(fs.FileSystemId),
				Name:            name,
				State:           string(fs.LifeCycleState),
				PerformanceMode: string(fs.PerformanceMode),
				Encrypted:       aws.ToBool(fs.Encrypted),
				MountTargets:    len(subnetIDs),
				SubnetIDs:       subnetIDs,
				VPCID:           vpcID,
			})
		}
		if out.NextMarker == nil {
			break
		}
		marker = out.NextMarker
	}
	return systems, nil
}

// ListEMRClusters returns the active EMR clusters launched into one of the
// VPC's subnets (EMR has no VPC field; attribution is by EC2 subnet).
func (c *VPCClient) ListEMRClusters(vpcSubnets map[string]bool) ([]EMRClusterInfo, error) {
	if len(vpcSubnets) == 0 {
		return nil, nil
	}
	ctx, cancel := c.requestContext()
	defer cancel()

	var clusters []EMRClusterInfo
	paginator := emr.NewListClustersPaginator(c.emr, &emr.ListClustersInput{
		ClusterStates: []emrtypes.ClusterState{
			emrtypes.ClusterStateStarting,
			emrtypes.ClusterStateBootstrapping,
			emrtypes.ClusterStateRunning,
			emrtypes.ClusterStateWaiting,
		},
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			if accessDenied(err) {
				return nil, nil
			}
			return nil, err
		}
		for _, cs := range page.Clusters {
			desc, err := c.emr.DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: cs.Id})
			if err != nil || desc.Cluster == nil {
				continue
			}
			cl := desc.Cluster
			subnet := ""
			if cl.Ec2InstanceAttributes != nil {
				subnet = aws.ToString(cl.Ec2InstanceAttributes.Ec2SubnetId)
				if !vpcSubnets[subnet] {
					// Instance-fleet clusters use RequestedEc2SubnetIds instead.
					if hit := firstInSet(cl.Ec2InstanceAttributes.RequestedEc2SubnetIds, vpcSubnets); hit != "" {
						subnet = hit
					}
				}
			}
			if !vpcSubnets[subnet] {
				continue
			}
			state := ""
			if cl.Status != nil {
				state = string(cl.Status.State)
			}
			clusters = append(clusters, EMRClusterInfo{
				ID:       aws.ToString(cl.Id),
				Name:     aws.ToString(cl.Name),
				State:    state,
				SubnetID: subnet,
			})
		}
	}
	return clusters, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// shortName returns the final path segment of an ARN or slash-separated name,
// e.g. "arn:aws:ecs:…:cluster/prod" → "prod".
func shortName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

func anyInSet(vals []string, set map[string]bool) bool {
	for _, v := range vals {
		if set[v] {
			return true
		}
	}
	return false
}

func firstInSet(vals []string, set map[string]bool) string {
	for _, v := range vals {
		if set[v] {
			return v
		}
	}
	return ""
}
