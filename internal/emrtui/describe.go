package emrtui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsec2 "github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

// ClusterDescription is a comprehensive, best-effort description of one EMR
// cluster: its configuration and OS, its compute layout (instance groups/fleets
// with their EC2 storage and resolved memory), its running EC2 instances, the
// services configured on it, and its VPC networking (subnet, security-group
// rules, route table, network ACL).
//
// Every section degrades independently: a denied or throttled API call records a
// note and leaves that one section empty rather than aborting the whole describe
// (the tool's best-effort principle). A blank field therefore reads as "unknown
// or none" only when no note explains it.
type ClusterDescription struct {
	Cluster Cluster // the enriched basics (also drives the console link)

	// Configuration & OS.
	ReleaseLabel       string
	OSReleaseLabel     string
	CustomAmiID        string
	EbsRootVolumeGiB   int32
	InstanceCollection string // INSTANCE_GROUP / INSTANCE_FLEET
	TerminationProt    *bool  // tri-state: nil when DescribeCluster didn't report it
	ScaleDownBehavior  string
	Applications       []AppInfo
	Configurations     []ConfigClassification

	// Compute, storage & memory: one entry per instance group (or fleet).
	Groups []NodeGroup

	// Running EC2 instances (ListInstances).
	Instances []Instance

	// Networking (VPC, subnet, security groups, routes, NACL).
	Network NetworkInfo

	// Notes records best-effort degradations (a denied/throttled call, or a
	// missing field) so a gap reads as "couldn't fetch" rather than "none".
	Notes []string
}

// ConfigClassification is one EMR configuration classification (e.g.
// spark-defaults) and its properties.
type ConfigClassification struct {
	Classification string            `json:"classification"`
	Properties     map[string]string `json:"properties,omitempty"`
}

// NodeGroup is one instance group (or fleet) in the cluster, with its storage
// and — once resolved from EC2 — its per-instance memory and vCPU.
type NodeGroup struct {
	ID           string      `json:"id,omitempty"`
	Name         string      `json:"name,omitempty"`
	Role         string      `json:"role"` // MASTER / CORE / TASK
	InstanceType string      `json:"instanceType,omitempty"`
	Market       string      `json:"market,omitempty"` // ON_DEMAND / SPOT
	Requested    int32       `json:"requested"`
	Running      int32       `json:"running"`
	State        string      `json:"state,omitempty"`
	EBSVolumes   []EBSVolume `json:"ebsVolumes,omitempty"`

	// Resolved EC2 instance-type specs (best effort); SpecsKnown is false when the
	// DescribeInstanceTypes lookup was denied or the type was not returned.
	VCPUs        int32  `json:"vcpus,omitempty"`
	MemoryMiB    int64  `json:"memoryMiB,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	SpecsKnown   bool   `json:"specsKnown"`
}

// EBSVolume is one EBS block device attached to every instance in a group.
type EBSVolume struct {
	Device     string `json:"device,omitempty"`
	VolumeType string `json:"volumeType,omitempty"`
	SizeGiB    int32  `json:"sizeGiB"`
	Iops       int32  `json:"iops,omitempty"`
}

// NetworkInfo is the cluster's VPC networking, gathered from EC2 for the
// cluster's subnet.
type NetworkInfo struct {
	SubnetID    string `json:"subnetId,omitempty"`
	VPCID       string `json:"vpcId,omitempty"`
	CIDR        string `json:"cidr,omitempty"`
	AZ          string `json:"availabilityZone,omitempty"`
	MapPublicIP *bool  `json:"mapPublicIpOnLaunch,omitempty"` // tri-state: nil when the subnet couldn't be described
	SubnetKnown bool   `json:"subnetKnown"`

	SecurityGroups []SecurityGroupRef `json:"securityGroups,omitempty"`
	Routes         []RouteEntry       `json:"routes,omitempty"`
	RouteTableID   string             `json:"routeTableId,omitempty"`
	NaclID         string             `json:"networkAclId,omitempty"`
	NaclEntries    []NaclEntry        `json:"networkAclEntries,omitempty"`

	// Note records why a sub-part is empty (denied call / no subnet on cluster).
	Note string `json:"note,omitempty"`
}

// SecurityGroupRef is one security group attached to the cluster, labelled by
// the role EMR assigns it, with its flattened rules.
type SecurityGroupRef struct {
	ID    string   `json:"id"`
	Name  string   `json:"name,omitempty"`
	Kind  string   `json:"kind"` // e.g. "EMR-managed (primary)", "additional (core/task)"
	Rules []SGRule `json:"rules,omitempty"`
	Known bool     `json:"known"` // false when DescribeSecurityGroups didn't return this group
}

// SGRule is one flattened security-group rule.
type SGRule struct {
	Direction string `json:"direction"` // inbound / outbound
	Protocol  string `json:"protocol"`
	Ports     string `json:"ports"`
	Source    string `json:"source"` // CIDR, prefix-list or referenced sg-id
}

// RouteEntry is one route in the subnet's effective route table.
type RouteEntry struct {
	Destination string `json:"destination"`
	Target      string `json:"target"`
	State       string `json:"state,omitempty"`
}

// NaclEntry is one network-ACL rule applied to the subnet.
type NaclEntry struct {
	Direction  string `json:"direction"` // inbound / outbound
	RuleNumber int32  `json:"ruleNumber"`
	Protocol   string `json:"protocol"`
	Ports      string `json:"ports"`
	CIDR       string `json:"cidr,omitempty"`
	Action     string `json:"action"` // allow / deny
}

// Describe gathers a comprehensive, best-effort description of one cluster. The
// only hard error is a failed DescribeCluster (the cluster basics); everything
// else degrades to a note so a partial describe is still useful.
func (c *Client) Describe(ctx context.Context, region, clusterID string) (ClusterDescription, error) {
	out, err := c.clientFor(region).DescribeCluster(ctx, &emr.DescribeClusterInput{ClusterId: aws.String(clusterID)})
	if err != nil {
		return ClusterDescription{}, err
	}
	if out.Cluster == nil {
		return ClusterDescription{}, fmt.Errorf("cluster %q not found", clusterID)
	}
	raw := out.Cluster

	d := ClusterDescription{}
	base := clusterFromDescribe(region, raw)
	applyClusterDetail(&base, raw)
	d.Cluster = base

	d.ReleaseLabel = aws.ToString(raw.ReleaseLabel)
	d.OSReleaseLabel = aws.ToString(raw.OSReleaseLabel)
	d.CustomAmiID = aws.ToString(raw.CustomAmiId)
	d.EbsRootVolumeGiB = aws.ToInt32(raw.EbsRootVolumeSize)
	d.InstanceCollection = string(raw.InstanceCollectionType)
	d.TerminationProt = raw.TerminationProtected
	d.ScaleDownBehavior = string(raw.ScaleDownBehavior)
	d.Applications = appInfosFrom(raw.Applications)
	d.Configurations = configClassifications(raw.Configurations)

	// Compute layout: instance groups, or fleets when the cluster uses them.
	d.Groups = c.loadGroups(ctx, region, clusterID, raw.InstanceCollectionType, &d.Notes)
	c.resolveInstanceSpecs(ctx, region, d.Groups, &d.Notes)

	// Running EC2 instances.
	if instances, err := c.Instances(ctx, region, clusterID, 0); err != nil {
		d.Notes = append(d.Notes, "EC2 instances unavailable (ListInstances denied/throttled)")
	} else {
		d.Instances = instances
	}

	// VPC networking for the cluster's subnet.
	d.Network = c.loadNetwork(ctx, region, raw)

	return d, nil
}

// clusterFromDescribe builds the dashboard's Cluster from a DescribeCluster
// result (the summary path is clusterFromSummary; this is its detail twin so the
// describe view can be opened on a cluster that was never listed via the table).
func clusterFromDescribe(region string, cl *emrtypes.Cluster) Cluster {
	c := Cluster{
		ID:           aws.ToString(cl.Id),
		Name:         aws.ToString(cl.Name),
		Region:       region,
		ARN:          aws.ToString(cl.ClusterArn),
		ReleaseLabel: aws.ToString(cl.ReleaseLabel),
	}
	if cl.NormalizedInstanceHours != nil {
		c.InstanceHours = aws.ToInt32(cl.NormalizedInstanceHours)
	}
	if cl.Status != nil {
		c.State = string(cl.Status.State)
		if cl.Status.Timeline != nil && cl.Status.Timeline.CreationDateTime != nil {
			c.Created = *cl.Status.Timeline.CreationDateTime
		}
	}
	return c
}

// appInfosFrom maps an EMR cluster's applications to AppInfo, skipping unnamed
// entries.
func appInfosFrom(apps []emrtypes.Application) []AppInfo {
	out := make([]AppInfo, 0, len(apps))
	for _, a := range apps {
		name := aws.ToString(a.Name)
		if name == "" {
			continue
		}
		out = append(out, AppInfo{Name: name, Version: aws.ToString(a.Version)})
	}
	return out
}

// configClassifications flattens a cluster's top-level configuration
// classifications (the common case — one level deep). Nested classifications are
// summarized by name only.
func configClassifications(cfgs []emrtypes.Configuration) []ConfigClassification {
	out := make([]ConfigClassification, 0, len(cfgs))
	for _, cfg := range cfgs {
		cls := aws.ToString(cfg.Classification)
		if cls == "" {
			continue
		}
		cc := ConfigClassification{Classification: cls, Properties: map[string]string{}}
		for k, v := range cfg.Properties {
			cc.Properties[k] = v
		}
		// Surface nested classifications as a property so they aren't lost.
		for _, nested := range cfg.Configurations {
			if n := aws.ToString(nested.Classification); n != "" {
				cc.Properties["(nested) "+n] = fmt.Sprintf("%d properties", len(nested.Properties))
			}
		}
		out = append(out, cc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Classification < out[j].Classification })
	return out
}

// loadGroups fetches the cluster's compute layout — instance groups, or fleets
// when InstanceCollectionType is INSTANCE_FLEET — best effort.
func (c *Client) loadGroups(ctx context.Context, region, clusterID string, collection emrtypes.InstanceCollectionType, notes *[]string) []NodeGroup {
	cl := c.clientFor(region)
	if collection == emrtypes.InstanceCollectionTypeInstanceFleet {
		var groups []NodeGroup
		pag := emr.NewListInstanceFleetsPaginator(cl, &emr.ListInstanceFleetsInput{ClusterId: aws.String(clusterID)})
		for pag.HasMorePages() {
			page, err := pag.NextPage(ctx)
			if err != nil {
				*notes = append(*notes, "instance fleets unavailable (ListInstanceFleets denied/throttled)")
				break
			}
			for _, f := range page.InstanceFleets {
				groups = append(groups, nodeGroupFromFleet(f))
			}
		}
		return groups
	}

	var groups []NodeGroup
	pag := emr.NewListInstanceGroupsPaginator(cl, &emr.ListInstanceGroupsInput{ClusterId: aws.String(clusterID)})
	for pag.HasMorePages() {
		page, err := pag.NextPage(ctx)
		if err != nil {
			*notes = append(*notes, "instance groups unavailable (ListInstanceGroups denied/throttled)")
			break
		}
		for _, g := range page.InstanceGroups {
			groups = append(groups, nodeGroupFromGroup(g))
		}
	}
	return groups
}

// nodeGroupFromGroup maps an EMR instance group to a NodeGroup. Pure — fixture
// tested.
func nodeGroupFromGroup(g emrtypes.InstanceGroup) NodeGroup {
	ng := NodeGroup{
		ID:           aws.ToString(g.Id),
		Name:         aws.ToString(g.Name),
		Role:         string(g.InstanceGroupType),
		InstanceType: aws.ToString(g.InstanceType),
		Market:       string(g.Market),
		Requested:    aws.ToInt32(g.RequestedInstanceCount),
		Running:      aws.ToInt32(g.RunningInstanceCount),
	}
	if g.Status != nil {
		ng.State = string(g.Status.State)
	}
	ng.EBSVolumes = ebsVolumesFrom(g.EbsBlockDevices)
	return ng
}

// nodeGroupFromFleet maps an EMR instance fleet to a NodeGroup. A fleet can mix
// instance types; the first type's specs/EBS represent the fleet, with the type
// list summarized in InstanceType.
func nodeGroupFromFleet(f emrtypes.InstanceFleet) NodeGroup {
	ng := NodeGroup{
		ID:        aws.ToString(f.Id),
		Name:      aws.ToString(f.Name),
		Role:      string(f.InstanceFleetType),
		Market:    "FLEET",
		Requested: aws.ToInt32(f.ProvisionedOnDemandCapacity) + aws.ToInt32(f.ProvisionedSpotCapacity),
	}
	if f.Status != nil {
		ng.State = string(f.Status.State)
	}
	types := make([]string, 0, len(f.InstanceTypeSpecifications))
	for i, s := range f.InstanceTypeSpecifications {
		types = append(types, aws.ToString(s.InstanceType))
		if i == 0 {
			ng.InstanceType = aws.ToString(s.InstanceType)
			ng.EBSVolumes = ebsVolumesFrom(s.EbsBlockDevices)
		}
	}
	if len(types) > 1 {
		ng.InstanceType = strings.Join(types, ", ")
	}
	return ng
}

// ebsVolumesFrom maps EMR EBS block devices to EBSVolume, skipping devices with
// no volume spec.
func ebsVolumesFrom(devices []emrtypes.EbsBlockDevice) []EBSVolume {
	out := make([]EBSVolume, 0, len(devices))
	for _, dev := range devices {
		if dev.VolumeSpecification == nil {
			continue
		}
		vs := dev.VolumeSpecification
		out = append(out, EBSVolume{
			Device:     aws.ToString(dev.Device),
			VolumeType: aws.ToString(vs.VolumeType),
			SizeGiB:    aws.ToInt32(vs.SizeInGB),
			Iops:       aws.ToInt32(vs.Iops),
		})
	}
	return out
}

// resolveInstanceSpecs fills each group's vCPU/memory/architecture from one
// batched ec2:DescribeInstanceTypes call. Best effort — a denied call leaves the
// groups' specs unknown (SpecsKnown=false), which the renderer shows as "—".
func (c *Client) resolveInstanceSpecs(ctx context.Context, region string, groups []NodeGroup, notes *[]string) {
	ec2c := c.ec2For(region)
	if ec2c == nil || len(groups) == 0 {
		return
	}
	// Collect the distinct, single instance types (skip fleet's multi-type list).
	want := map[string]bool{}
	var types []ec2types.InstanceType
	for _, g := range groups {
		t := g.InstanceType
		if t == "" || strings.Contains(t, ",") || want[t] {
			continue
		}
		want[t] = true
		types = append(types, ec2types.InstanceType(t))
	}
	if len(types) == 0 {
		return
	}
	specs := map[string]NodeGroup{} // reuse NodeGroup as a spec carrier
	pag := awsec2.NewDescribeInstanceTypesPaginator(ec2c, &awsec2.DescribeInstanceTypesInput{InstanceTypes: types})
	for pag.HasMorePages() {
		page, err := pag.NextPage(ctx)
		if err != nil {
			*notes = append(*notes, "instance-type specs unavailable (DescribeInstanceTypes denied/throttled)")
			break
		}
		for _, it := range page.InstanceTypes {
			s := NodeGroup{SpecsKnown: true}
			if it.VCpuInfo != nil {
				s.VCPUs = aws.ToInt32(it.VCpuInfo.DefaultVCpus)
			}
			if it.MemoryInfo != nil {
				s.MemoryMiB = aws.ToInt64(it.MemoryInfo.SizeInMiB)
			}
			if it.ProcessorInfo != nil && len(it.ProcessorInfo.SupportedArchitectures) > 0 {
				s.Architecture = string(it.ProcessorInfo.SupportedArchitectures[0])
			}
			specs[string(it.InstanceType)] = s
		}
	}
	for i := range groups {
		if s, ok := specs[groups[i].InstanceType]; ok {
			groups[i].VCPUs = s.VCPUs
			groups[i].MemoryMiB = s.MemoryMiB
			groups[i].Architecture = s.Architecture
			groups[i].SpecsKnown = true
		}
	}
}

// loadNetwork describes the cluster's subnet and the security groups, route
// table and network ACL around it. Best effort throughout — a denied call sets
// the section's Note and leaves the rest of the describe intact.
func (c *Client) loadNetwork(ctx context.Context, region string, raw *emrtypes.Cluster) NetworkInfo {
	var net NetworkInfo
	ec2c := c.ec2For(region)
	attrs := raw.Ec2InstanceAttributes
	if attrs == nil {
		net.Note = "cluster reported no EC2 instance attributes"
		return net
	}
	net.SubnetID = aws.ToString(attrs.Ec2SubnetId)
	if net.SubnetID == "" && len(attrs.RequestedEc2SubnetIds) > 0 {
		net.SubnetID = attrs.RequestedEc2SubnetIds[0]
	}
	net.AZ = aws.ToString(attrs.Ec2AvailabilityZone)
	net.SecurityGroups = securityGroupRefs(attrs)

	if ec2c == nil {
		net.Note = "EC2 client unavailable for networking"
		return net
	}

	// Subnet → VPC, CIDR, public-IP behaviour.
	if net.SubnetID != "" {
		if sub, err := ec2c.DescribeSubnets(ctx, &awsec2.DescribeSubnetsInput{SubnetIds: []string{net.SubnetID}}); err != nil {
			net.Note = appendNote(net.Note, "subnet detail unavailable (DescribeSubnets denied/throttled)")
		} else if len(sub.Subnets) > 0 {
			s := sub.Subnets[0]
			net.VPCID = aws.ToString(s.VpcId)
			net.CIDR = aws.ToString(s.CidrBlock)
			if net.AZ == "" {
				net.AZ = aws.ToString(s.AvailabilityZone)
			}
			net.MapPublicIP = s.MapPublicIpOnLaunch
			net.SubnetKnown = true
		}
	}

	// Security-group rules.
	c.fillSecurityGroupRules(ctx, ec2c, net.SecurityGroups, &net)

	// Route table & NACL for the subnet (need the subnet/VPC to scope them).
	if net.SubnetID != "" {
		c.fillRouteTable(ctx, ec2c, net.SubnetID, net.VPCID, &net)
		c.fillNetworkACL(ctx, ec2c, net.SubnetID, &net)
	}
	return net
}

// securityGroupRefs collects the cluster's security groups, labelled by the role
// EMR assigns each, in a stable order (managed first, then additional).
func securityGroupRefs(attrs *emrtypes.Ec2InstanceAttributes) []SecurityGroupRef {
	var refs []SecurityGroupRef
	add := func(id, kind string) {
		if id == "" {
			return
		}
		refs = append(refs, SecurityGroupRef{ID: id, Kind: kind})
	}
	add(aws.ToString(attrs.EmrManagedMasterSecurityGroup), "EMR-managed (primary)")
	add(aws.ToString(attrs.EmrManagedSlaveSecurityGroup), "EMR-managed (core/task)")
	add(aws.ToString(attrs.ServiceAccessSecurityGroup), "service access")
	for _, id := range attrs.AdditionalMasterSecurityGroups {
		add(id, "additional (primary)")
	}
	for _, id := range attrs.AdditionalSlaveSecurityGroups {
		add(id, "additional (core/task)")
	}
	return refs
}

// fillSecurityGroupRules describes the referenced security groups in one call
// and attaches their flattened rules.
func (c *Client) fillSecurityGroupRules(ctx context.Context, ec2c *awsec2.Client, refs []SecurityGroupRef, net *NetworkInfo) {
	if len(refs) == 0 {
		return
	}
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		ids = append(ids, r.ID)
	}
	out, err := ec2c.DescribeSecurityGroups(ctx, &awsec2.DescribeSecurityGroupsInput{GroupIds: ids})
	if err != nil {
		net.Note = appendNote(net.Note, "security-group rules unavailable (DescribeSecurityGroups denied/throttled)")
		return
	}
	byID := map[string]ec2types.SecurityGroup{}
	for _, sg := range out.SecurityGroups {
		byID[aws.ToString(sg.GroupId)] = sg
	}
	for i := range net.SecurityGroups {
		sg, ok := byID[net.SecurityGroups[i].ID]
		if !ok {
			continue
		}
		net.SecurityGroups[i].Name = aws.ToString(sg.GroupName)
		net.SecurityGroups[i].Known = true
		for _, p := range sg.IpPermissions {
			net.SecurityGroups[i].Rules = append(net.SecurityGroups[i].Rules, sgRulesFromPerm(p, "inbound")...)
		}
		for _, p := range sg.IpPermissionsEgress {
			net.SecurityGroups[i].Rules = append(net.SecurityGroups[i].Rules, sgRulesFromPerm(p, "outbound")...)
		}
	}
}

// sgRulesFromPerm flattens one IP permission into per-source rules.
func sgRulesFromPerm(p ec2types.IpPermission, dir string) []SGRule {
	proto := protocolLabel(aws.ToString(p.IpProtocol))
	ports := portLabel(aws.ToString(p.IpProtocol), p.FromPort, p.ToPort)
	var rules []SGRule
	for _, r := range p.IpRanges {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, Ports: ports, Source: aws.ToString(r.CidrIp)})
	}
	for _, r := range p.Ipv6Ranges {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, Ports: ports, Source: aws.ToString(r.CidrIpv6)})
	}
	for _, r := range p.PrefixListIds {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, Ports: ports, Source: aws.ToString(r.PrefixListId)})
	}
	for _, g := range p.UserIdGroupPairs {
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, Ports: ports, Source: aws.ToString(g.GroupId)})
	}
	if len(rules) == 0 {
		// A permission with no sources still describes an opening; record it.
		rules = append(rules, SGRule{Direction: dir, Protocol: proto, Ports: ports, Source: "—"})
	}
	return rules
}

// fillRouteTable finds the route table effective for the subnet: the explicitly
// associated table, falling back to the VPC's main table.
func (c *Client) fillRouteTable(ctx context.Context, ec2c *awsec2.Client, subnetID, vpcID string, net *NetworkInfo) {
	out, err := ec2c.DescribeRouteTables(ctx, &awsec2.DescribeRouteTablesInput{
		Filters: []ec2types.Filter{{Name: aws.String("association.subnet-id"), Values: []string{subnetID}}},
	})
	if err != nil {
		net.Note = appendNote(net.Note, "route table unavailable (DescribeRouteTables denied/throttled)")
		return
	}
	rt, ok := pickRouteTable(out.RouteTables, subnetID)
	if !ok && vpcID != "" {
		// No explicit association → the subnet uses the VPC's main route table.
		mainOut, merr := ec2c.DescribeRouteTables(ctx, &awsec2.DescribeRouteTablesInput{
			Filters: []ec2types.Filter{
				{Name: aws.String("vpc-id"), Values: []string{vpcID}},
				{Name: aws.String("association.main"), Values: []string{"true"}},
			},
		})
		if merr == nil {
			rt, ok = pickRouteTable(mainOut.RouteTables, subnetID)
		}
	}
	if !ok {
		return
	}
	net.RouteTableID = aws.ToString(rt.RouteTableId)
	net.Routes = routeEntries(rt)
}

// pickRouteTable returns the first route table (subnet-scoped lists already
// filter to the right one).
func pickRouteTable(tables []ec2types.RouteTable, _ string) (ec2types.RouteTable, bool) {
	if len(tables) == 0 {
		return ec2types.RouteTable{}, false
	}
	return tables[0], true
}

// routeEntries flattens a route table's routes.
func routeEntries(rt ec2types.RouteTable) []RouteEntry {
	out := make([]RouteEntry, 0, len(rt.Routes))
	for _, r := range rt.Routes {
		dest := aws.ToString(r.DestinationCidrBlock)
		if dest == "" {
			dest = aws.ToString(r.DestinationIpv6CidrBlock)
		}
		if dest == "" {
			dest = aws.ToString(r.DestinationPrefixListId)
		}
		out = append(out, RouteEntry{Destination: dest, Target: routeTargetLabel(r), State: string(r.State)})
	}
	return out
}

// routeTargetLabel renders the first non-empty target of a route.
func routeTargetLabel(r ec2types.Route) string {
	switch {
	case aws.ToString(r.GatewayId) != "":
		return aws.ToString(r.GatewayId)
	case aws.ToString(r.NatGatewayId) != "":
		return aws.ToString(r.NatGatewayId)
	case aws.ToString(r.TransitGatewayId) != "":
		return aws.ToString(r.TransitGatewayId)
	case aws.ToString(r.VpcPeeringConnectionId) != "":
		return aws.ToString(r.VpcPeeringConnectionId)
	case aws.ToString(r.NetworkInterfaceId) != "":
		return aws.ToString(r.NetworkInterfaceId)
	case aws.ToString(r.InstanceId) != "":
		return aws.ToString(r.InstanceId)
	case aws.ToString(r.CarrierGatewayId) != "":
		return aws.ToString(r.CarrierGatewayId)
	case aws.ToString(r.LocalGatewayId) != "":
		return aws.ToString(r.LocalGatewayId)
	default:
		return "local"
	}
}

// fillNetworkACL finds the network ACL associated with the subnet.
func (c *Client) fillNetworkACL(ctx context.Context, ec2c *awsec2.Client, subnetID string, net *NetworkInfo) {
	out, err := ec2c.DescribeNetworkAcls(ctx, &awsec2.DescribeNetworkAclsInput{
		Filters: []ec2types.Filter{{Name: aws.String("association.subnet-id"), Values: []string{subnetID}}},
	})
	if err != nil {
		net.Note = appendNote(net.Note, "network ACL unavailable (DescribeNetworkAcls denied/throttled)")
		return
	}
	if len(out.NetworkAcls) == 0 {
		return
	}
	nacl := out.NetworkAcls[0]
	net.NaclID = aws.ToString(nacl.NetworkAclId)
	net.NaclEntries = naclEntries(nacl)
}

// naclEntries flattens a network ACL's rules, sorted by direction then rule
// number so the deny/allow ordering reads as AWS evaluates it.
func naclEntries(nacl ec2types.NetworkAcl) []NaclEntry {
	out := make([]NaclEntry, 0, len(nacl.Entries))
	for _, e := range nacl.Entries {
		dir := "inbound"
		if aws.ToBool(e.Egress) {
			dir = "outbound"
		}
		cidr := aws.ToString(e.CidrBlock)
		if cidr == "" {
			cidr = aws.ToString(e.Ipv6CidrBlock)
		}
		out = append(out, NaclEntry{
			Direction:  dir,
			RuleNumber: aws.ToInt32(e.RuleNumber),
			Protocol:   naclProtocolLabel(aws.ToString(e.Protocol)),
			Ports:      naclPortLabel(e.PortRange),
			CIDR:       cidr,
			Action:     string(e.RuleAction),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Direction != out[j].Direction {
			return out[i].Direction < out[j].Direction // inbound before outbound
		}
		return out[i].RuleNumber < out[j].RuleNumber
	})
	return out
}

// --- small label helpers (pure) -------------------------------------------

// protocolLabel renders a security-group protocol number; "-1" is all
// protocols.
func protocolLabel(p string) string {
	switch p {
	case "-1", "":
		return "all"
	case "6":
		return "tcp"
	case "17":
		return "udp"
	case "1":
		return "icmp"
	default:
		return p
	}
}

// portLabel renders a security-group port range. For the all-protocols rule the
// ports are irrelevant ("all").
func portLabel(proto string, from, to *int32) string {
	if proto == "-1" || proto == "" {
		return "all"
	}
	if from == nil && to == nil {
		return "all"
	}
	f, t := aws.ToInt32(from), aws.ToInt32(to)
	if f == 0 && t == 0 {
		return "all"
	}
	if f == t {
		return itoa(int(f))
	}
	return itoa(int(f)) + "-" + itoa(int(t))
}

// naclProtocolLabel maps a NACL protocol number to a name ("-1" is all).
func naclProtocolLabel(p string) string {
	switch p {
	case "-1", "":
		return "all"
	case "6":
		return "tcp"
	case "17":
		return "udp"
	case "1":
		return "icmp"
	default:
		return p
	}
}

// naclPortLabel renders a NACL port range, or "all" when unset.
func naclPortLabel(pr *ec2types.PortRange) string {
	if pr == nil {
		return "all"
	}
	f, t := aws.ToInt32(pr.From), aws.ToInt32(pr.To)
	if f == 0 && t == 0 {
		return "all"
	}
	if f == t {
		return itoa(int(f))
	}
	return itoa(int(f)) + "-" + itoa(int(t))
}

// appendNote joins two best-effort notes with "; ".
func appendNote(existing, add string) string {
	if existing == "" {
		return add
	}
	return existing + "; " + add
}
