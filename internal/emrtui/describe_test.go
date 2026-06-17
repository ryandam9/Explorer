package emrtui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

func TestNodeGroupFromGroup(t *testing.T) {
	g := emrtypes.InstanceGroup{
		Id:                     aws.String("ig-1"),
		Name:                   aws.String("Core"),
		InstanceGroupType:      emrtypes.InstanceGroupTypeCore,
		InstanceType:           aws.String("r5.2xlarge"),
		Market:                 emrtypes.MarketTypeSpot,
		RequestedInstanceCount: aws.Int32(4),
		RunningInstanceCount:   aws.Int32(3),
		Status:                 &emrtypes.InstanceGroupStatus{State: emrtypes.InstanceGroupStateRunning},
		EbsBlockDevices: []emrtypes.EbsBlockDevice{
			{Device: aws.String("/dev/sdb"), VolumeSpecification: &emrtypes.VolumeSpecification{
				VolumeType: aws.String("gp3"), SizeInGB: aws.Int32(256), Iops: aws.Int32(3000)}},
			{Device: aws.String("/dev/sdc")}, // no spec → skipped
		},
	}
	ng := nodeGroupFromGroup(g)
	if ng.Role != "CORE" || ng.InstanceType != "r5.2xlarge" || ng.Market != "SPOT" {
		t.Errorf("group mapping wrong: %+v", ng)
	}
	if ng.Requested != 4 || ng.Running != 3 || ng.State != "RUNNING" {
		t.Errorf("counts/state wrong: %+v", ng)
	}
	if len(ng.EBSVolumes) != 1 {
		t.Fatalf("expected 1 EBS volume (the spec-less device is skipped), got %d", len(ng.EBSVolumes))
	}
	v := ng.EBSVolumes[0]
	if v.VolumeType != "gp3" || v.SizeGiB != 256 || v.Iops != 3000 {
		t.Errorf("ebs volume mapping wrong: %+v", v)
	}
}

func TestNodeGroupFromFleetMultiType(t *testing.T) {
	f := emrtypes.InstanceFleet{
		Id:                          aws.String("if-1"),
		Name:                        aws.String("CoreFleet"),
		InstanceFleetType:           emrtypes.InstanceFleetTypeCore,
		ProvisionedOnDemandCapacity: aws.Int32(2),
		ProvisionedSpotCapacity:     aws.Int32(3),
		Status:                      &emrtypes.InstanceFleetStatus{State: emrtypes.InstanceFleetStateRunning},
		InstanceTypeSpecifications: []emrtypes.InstanceTypeSpecification{
			{InstanceType: aws.String("m5.xlarge"), EbsBlockDevices: []emrtypes.EbsBlockDevice{
				{Device: aws.String("/dev/sdb"), VolumeSpecification: &emrtypes.VolumeSpecification{
					VolumeType: aws.String("gp3"), SizeInGB: aws.Int32(64)}}}},
			{InstanceType: aws.String("m5.2xlarge")},
		},
	}
	ng := nodeGroupFromFleet(f)
	if ng.Role != "CORE" || ng.Market != "FLEET" {
		t.Errorf("fleet mapping wrong: %+v", ng)
	}
	if ng.Requested != 5 {
		t.Errorf("fleet capacity = %d, want 5 (on-demand + spot)", ng.Requested)
	}
	if !strings.Contains(ng.InstanceType, "m5.xlarge") || !strings.Contains(ng.InstanceType, "m5.2xlarge") {
		t.Errorf("multi-type fleet should list both types, got %q", ng.InstanceType)
	}
	// EBS is taken from the first type spec.
	if len(ng.EBSVolumes) != 1 || ng.EBSVolumes[0].SizeGiB != 64 {
		t.Errorf("fleet EBS mapping wrong: %+v", ng.EBSVolumes)
	}
}

func TestConfigClassifications(t *testing.T) {
	cfgs := []emrtypes.Configuration{
		{Classification: aws.String("spark-defaults"), Properties: map[string]string{"spark.executor.memory": "4g"}},
		{Classification: aws.String("core-site"), Configurations: []emrtypes.Configuration{
			{Classification: aws.String("nested"), Properties: map[string]string{"a": "1", "b": "2"}}}},
		{Properties: map[string]string{"x": "y"}}, // no classification → skipped
	}
	out := configClassifications(cfgs)
	if len(out) != 2 {
		t.Fatalf("expected 2 classifications (unnamed skipped), got %d", len(out))
	}
	// Sorted: core-site before spark-defaults.
	if out[0].Classification != "core-site" {
		t.Errorf("classifications should be sorted, got first = %q", out[0].Classification)
	}
	if got := out[0].Properties["(nested) nested"]; got != "2 properties" {
		t.Errorf("nested classification not surfaced, got %q", got)
	}
	if out[1].Properties["spark.executor.memory"] != "4g" {
		t.Errorf("spark property lost: %+v", out[1].Properties)
	}
}

func TestSecurityGroupRefsOrderAndKinds(t *testing.T) {
	attrs := &emrtypes.Ec2InstanceAttributes{
		EmrManagedMasterSecurityGroup:  aws.String("sg-m"),
		EmrManagedSlaveSecurityGroup:   aws.String("sg-s"),
		ServiceAccessSecurityGroup:     aws.String("sg-svc"),
		AdditionalMasterSecurityGroups: []string{"sg-am"},
		AdditionalSlaveSecurityGroups:  []string{"sg-as"},
	}
	refs := securityGroupRefs(attrs)
	want := []struct{ id, kind string }{
		{"sg-m", "EMR-managed (primary)"},
		{"sg-s", "EMR-managed (core/task)"},
		{"sg-svc", "service access"},
		{"sg-am", "additional (primary)"},
		{"sg-as", "additional (core/task)"},
	}
	if len(refs) != len(want) {
		t.Fatalf("got %d refs, want %d", len(refs), len(want))
	}
	for i, w := range want {
		if refs[i].ID != w.id || refs[i].Kind != w.kind {
			t.Errorf("ref %d = %+v, want %s/%s", i, refs[i], w.id, w.kind)
		}
	}
	// An empty attribute set yields no refs (no blank IDs).
	if got := securityGroupRefs(&emrtypes.Ec2InstanceAttributes{}); len(got) != 0 {
		t.Errorf("empty attrs should yield no SG refs, got %+v", got)
	}
}

func TestSGRulesFromPerm(t *testing.T) {
	// A TCP permission with a CIDR and a referenced SG → two flattened rules.
	perm := ec2types.IpPermission{
		IpProtocol: aws.String("6"),
		FromPort:   aws.Int32(8088),
		ToPort:     aws.Int32(8088),
		IpRanges:   []ec2types.IpRange{{CidrIp: aws.String("10.0.0.0/16")}},
		UserIdGroupPairs: []ec2types.UserIdGroupPair{
			{GroupId: aws.String("sg-peer")}},
	}
	rules := sgRulesFromPerm(perm, "inbound")
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d: %+v", len(rules), rules)
	}
	if rules[0].Protocol != "tcp" || rules[0].Ports != "8088" || rules[0].Source != "10.0.0.0/16" {
		t.Errorf("cidr rule wrong: %+v", rules[0])
	}
	if rules[1].Source != "sg-peer" {
		t.Errorf("sg-ref rule wrong: %+v", rules[1])
	}

	// An all-protocols permission with no sources still records one opening.
	all := sgRulesFromPerm(ec2types.IpPermission{IpProtocol: aws.String("-1")}, "outbound")
	if len(all) != 1 || all[0].Protocol != "all" || all[0].Ports != "all" || all[0].Source != "—" {
		t.Errorf("all-protocols rule wrong: %+v", all)
	}
}

func TestNaclEntriesSortedByDirectionThenRule(t *testing.T) {
	nacl := ec2types.NetworkAcl{
		NetworkAclId: aws.String("acl-1"),
		Entries: []ec2types.NetworkAclEntry{
			{Egress: aws.Bool(true), RuleNumber: aws.Int32(100), Protocol: aws.String("-1"), RuleAction: ec2types.RuleActionAllow, CidrBlock: aws.String("0.0.0.0/0")},
			{Egress: aws.Bool(false), RuleNumber: aws.Int32(200), Protocol: aws.String("6"), RuleAction: ec2types.RuleActionDeny, CidrBlock: aws.String("10.0.0.0/8"), PortRange: &ec2types.PortRange{From: aws.Int32(22), To: aws.Int32(22)}},
			{Egress: aws.Bool(false), RuleNumber: aws.Int32(100), Protocol: aws.String("-1"), RuleAction: ec2types.RuleActionAllow, CidrBlock: aws.String("0.0.0.0/0")},
		},
	}
	entries := naclEntries(nacl)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	// inbound 100, inbound 200, then outbound 100.
	if entries[0].Direction != "inbound" || entries[0].RuleNumber != 100 {
		t.Errorf("entry 0 = %+v, want inbound 100", entries[0])
	}
	if entries[1].Direction != "inbound" || entries[1].RuleNumber != 200 || entries[1].Protocol != "tcp" || entries[1].Ports != "22" || entries[1].Action != "deny" {
		t.Errorf("entry 1 = %+v, want inbound 200 tcp/22 deny", entries[1])
	}
	if entries[2].Direction != "outbound" || entries[2].RuleNumber != 100 {
		t.Errorf("entry 2 = %+v, want outbound 100", entries[2])
	}
}

func TestRouteTargetLabel(t *testing.T) {
	cases := []struct {
		r    ec2types.Route
		want string
	}{
		{ec2types.Route{GatewayId: aws.String("igw-1")}, "igw-1"},
		{ec2types.Route{NatGatewayId: aws.String("nat-1")}, "nat-1"},
		{ec2types.Route{TransitGatewayId: aws.String("tgw-1")}, "tgw-1"},
		{ec2types.Route{}, "local"},
	}
	for _, c := range cases {
		if got := routeTargetLabel(c.r); got != c.want {
			t.Errorf("routeTargetLabel(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}

func TestProtocolAndPortLabels(t *testing.T) {
	if protocolLabel("-1") != "all" || protocolLabel("6") != "tcp" || protocolLabel("17") != "udp" || protocolLabel("50") != "50" {
		t.Error("protocolLabel mapping wrong")
	}
	if portLabel("6", aws.Int32(80), aws.Int32(80)) != "80" {
		t.Error("single port wrong")
	}
	if portLabel("6", aws.Int32(1024), aws.Int32(2048)) != "1024-2048" {
		t.Error("port range wrong")
	}
	if portLabel("-1", aws.Int32(80), aws.Int32(80)) != "all" {
		t.Error("all-protocol ports should read 'all'")
	}
}

func TestDescribeSectionsAndTriState(t *testing.T) {
	yes := true
	d := ClusterDescription{
		Cluster:         Cluster{Name: "prod", ID: "j-1", Region: "us-east-1", State: "WAITING", AutoTerminate: false},
		ReleaseLabel:    "emr-7.1.0",
		OSReleaseLabel:  "2.0.20240131.0",
		TerminationProt: &yes,
		Applications:    []AppInfo{{Name: "Spark", Version: "3.5.0"}},
		Groups: []NodeGroup{{Role: "MASTER", InstanceType: "m5.xlarge", Running: 1, Requested: 1,
			VCPUs: 4, MemoryMiB: 16384, Architecture: "x86_64", SpecsKnown: true,
			EBSVolumes: []EBSVolume{{VolumeType: "gp3", SizeGiB: 64}}}},
		Network: NetworkInfo{SubnetID: "subnet-1", VPCID: "vpc-1", CIDR: "10.0.0.0/24"},
		Notes:   []string{"instance fleets unavailable"},
	}
	secs := d.sections()
	titles := map[string]string{}
	for _, s := range secs {
		titles[s.Title] = s.Body
	}
	for _, want := range []string{"Overview", "Configuration & OS", "Services", "Compute, memory & storage", "EC2 instances", "Networking", "Notes"} {
		if _, ok := titles[want]; !ok {
			t.Errorf("missing section %q", want)
		}
	}
	if !strings.Contains(titles["Configuration & OS"], "Amazon Linux 2.0.20240131.0") {
		t.Errorf("OS label missing:\n%s", titles["Configuration & OS"])
	}
	if !strings.Contains(titles["Compute, memory & storage"], "16.0 GiB") {
		t.Errorf("memory label missing:\n%s", titles["Compute, memory & storage"])
	}
	if !strings.Contains(titles["Compute, memory & storage"], "gp3 64 GiB") {
		t.Errorf("EBS label missing:\n%s", titles["Compute, memory & storage"])
	}
	if !strings.Contains(titles["Networking"], "vpc-1") {
		t.Errorf("VPC missing from networking:\n%s", titles["Networking"])
	}
	if !strings.Contains(titles["Notes"], "instance fleets unavailable") {
		t.Errorf("notes missing:\n%s", titles["Notes"])
	}
}

func TestMemoryLabelUnknownVsValue(t *testing.T) {
	// Specs unknown (denied DescribeInstanceTypes) → em dash, not a fake 0.
	if got := memoryLabel(NodeGroup{InstanceType: "m5.xlarge", SpecsKnown: false}); got != "—" {
		t.Errorf("unknown memory = %q, want em dash", got)
	}
	if got := memoryLabel(NodeGroup{MemoryMiB: 8192, SpecsKnown: true}); got != "8.0 GiB" {
		t.Errorf("memory = %q, want 8.0 GiB", got)
	}
}

func TestTriStateLabel(t *testing.T) {
	yes, no := true, false
	if triStateLabel(nil) != "unknown" || triStateLabel(&yes) != "yes" || triStateLabel(&no) != "no" {
		t.Error("triStateLabel mapping wrong")
	}
}

func TestRenderDescribeTextAndJSON(t *testing.T) {
	d := richTestDescription(Cluster{Name: "prod", ID: "j-1", Region: "us-east-1", State: "WAITING"})

	// Text (default/table) output is the sectioned report without colour.
	var text bytes.Buffer
	if err := RenderDescribe(&text, d, "table"); err != nil {
		t.Fatalf("text render: %v", err)
	}
	for _, want := range []string{"Overview", "Networking", "sg-master", "16.0 GiB", "vpc-abc"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, text.String())
		}
	}

	// JSON output round-trips into the machine shape with stable keys.
	var buf bytes.Buffer
	if err := RenderDescribe(&buf, d, "json"); err != nil {
		t.Fatalf("json render: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json invalid: %v\n%s", err, buf.String())
	}
	if got["id"] != "j-1" || got["releaseLabel"] != "emr-7.1.0" {
		t.Errorf("json basics wrong: %v", got)
	}
	net, ok := got["network"].(map[string]any)
	if !ok || net["vpcId"] != "vpc-abc" {
		t.Errorf("json network missing vpcId: %v", got["network"])
	}
	groups, ok := got["instanceGroups"].([]any)
	if !ok || len(groups) != 2 {
		t.Fatalf("json instanceGroups wrong: %v", got["instanceGroups"])
	}
}
