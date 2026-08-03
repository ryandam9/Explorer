package emrtui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func sampleDescription() ClusterDescription {
	return ClusterDescription{
		Cluster: Cluster{
			ID: "j-ABC123", Name: "prod-etl", Region: "ap-southeast-2",
			ARN:   "arn:aws:elasticmapreduce:ap-southeast-2:123456789012:cluster/j-ABC123",
			State: "WAITING", Created: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
			InstanceHours: 128, MasterDNS: "ip-10-0-1-5.ec2.internal",
			LogURI: "s3://logs/emr/", ServiceRole: "EMR_DefaultRole",
			InstanceProfile: "EMR_EC2_DefaultRole", KeyName: "prod-key",
		},
		ReleaseLabel:       "emr-7.3.0",
		OSReleaseLabel:     "2023.5.20260601",
		EbsRootVolumeGiB:   30,
		InstanceCollection: "INSTANCE_GROUP",
		TerminationProt:    aws.Bool(true),
		ScaleDownBehavior:  "TERMINATE_AT_TASK_COMPLETION",
		Applications:       []AppInfo{{Name: "Spark", Version: "3.5.1"}, {Name: "Hadoop", Version: "3.3.6"}},
		Configurations: []ConfigClassification{
			{Classification: "spark-defaults", Properties: map[string]string{
				"spark.executor.memory": "4g",
				"spark.pipe|test":       "a|b", // pipes must be escaped in cells
			}},
		},
		Groups: []NodeGroup{
			{Role: "MASTER", Name: "Master", InstanceType: "m5.xlarge", Market: "ON_DEMAND",
				Requested: 1, Running: 1, State: "RUNNING", VCPUs: 4, MemoryMiB: 16384, SpecsKnown: true,
				EBSVolumes: []EBSVolume{{Device: "/dev/sdb", VolumeType: "gp3", SizeGiB: 100}}},
			{Role: "CORE", InstanceType: "r5.2xlarge", Market: "SPOT", Requested: 4, Running: 3},
		},
		Instances: []Instance{
			{EC2ID: "i-0abc", Type: "m5.xlarge", Market: "ON_DEMAND", State: "RUNNING",
				Group: "ig-master", PrivateDNS: "ip-10-0-1-5.ec2.internal"},
		},
		Network: NetworkInfo{
			SubnetID: "subnet-1", VPCID: "vpc-1", CIDR: "10.0.1.0/24", AZ: "ap-southeast-2a",
			MapPublicIP: aws.Bool(false), SubnetKnown: true,
			SecurityGroups: []SecurityGroupRef{{
				ID: "sg-1", Name: "emr-primary", Kind: "EMR-managed (primary)", Known: true,
				Rules: []SGRule{{Direction: "inbound", Protocol: "tcp", Ports: "8443", Source: "sg-2"}},
			}},
			RouteTableID: "rtb-1",
			Routes:       []RouteEntry{{Destination: "0.0.0.0/0", Target: "nat-1", State: "active"}},
			NaclID:       "acl-1",
			NaclEntries:  []NaclEntry{{Direction: "inbound", RuleNumber: 100, Protocol: "-1", Ports: "all", CIDR: "0.0.0.0/0", Action: "allow"}},
		},
		Notes: []string{"EC2 instances truncated (ListInstances throttled)"},
	}
}

func TestClusterMarkdownSections(t *testing.T) {
	md := clusterMarkdown(sampleDescription(), time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC))

	for _, want := range []string{
		"# EMR cluster prod-etl (j-ABC123)",
		"## Overview",
		"## Configuration & OS",
		"## S3 connector",
		"## Applications",
		"## Compute, memory & storage",
		"## EC2 instances",
		"## Networking",
		"### Security groups",
		"### Route table rtb-1",
		"### Network ACL acl-1",
		"## Configurations",
		"### spark-defaults",
		"## Notes",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing section %q", want)
		}
	}

	for _, want := range []string{
		"| **Release** | emr-7.3.0 |",
		"| Spark | 3.5.1 |",
		"| MASTER | Master | m5.xlarge | ON_DEMAND | 1 / 1 | 4 | 16.0 GiB | RUNNING | gp3 100 GiB |",
		"| inbound | tcp | 8443 | sg-2 |",
		"| 0.0.0.0/0 | nat-1 | active |",
		"- ⚠ EC2 instances truncated (ListInstances throttled)",
		"| **Termination protection** | yes |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing content %q", want)
		}
	}

	// Pipes inside values must be escaped so table cells don't split.
	if !strings.Contains(md, `spark.pipe\|test`) || !strings.Contains(md, `a\|b`) {
		t.Error("pipes in configuration values must be escaped for Markdown tables")
	}

	// The CORE group has no resolved specs: unknown renders as an em dash, not 0.
	if !strings.Contains(md, "| CORE | — | r5.2xlarge | SPOT | 3 / 4 | — | — | — |") {
		t.Error("unresolved instance specs should render as em dashes")
	}
}

func TestExportClusterMarkdownWritesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	path, err := exportClusterMarkdown(sampleDescription())
	if err != nil {
		t.Fatalf("exportClusterMarkdown error: %v", err)
	}
	if !strings.Contains(path, "emr-j-ABC123-") || !strings.HasSuffix(path, ".md") {
		t.Errorf("unexpected export path %q", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading export: %v", err)
	}
	if !strings.Contains(string(b), "# EMR cluster prod-etl (j-ABC123)") {
		t.Error("exported file missing the report heading")
	}
}
