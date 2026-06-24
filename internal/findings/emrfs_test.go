package findings

import (
	"strings"
	"testing"
	"time"
)

func TestEMRReleaseAtLeast(t *testing.T) {
	cases := []struct {
		label   string
		atLeast bool
		known   bool
	}{
		{"emr-7.10.0", true, true},
		{"emr-7.11.0", true, true},
		{"emr-8.0.0", true, true},
		{"emr-7.9.0", false, true},
		{"emr-6.15.0", false, true},
		{"EMR-7.10.0", true, true}, // case-insensitive
		{"7.10.0", true, true},     // prefix optional
		{"", false, false},
		{"garbage", false, false},
		{"emr-x.y", false, false},
	}
	for _, c := range cases {
		at, known := emrReleaseAtLeast(c.label, s3aDefaultMajor, s3aDefaultMinor)
		if at != c.atLeast || known != c.known {
			t.Errorf("emrReleaseAtLeast(%q) = (%v,%v), want (%v,%v)", c.label, at, known, c.atLeast, c.known)
		}
	}
}

func TestDeriveS3Connector_ReleaseDefault(t *testing.T) {
	s3a := DeriveS3Connector(S3ConnectorInput{ReleaseLabel: "emr-7.10.0"})
	if s3a.Effective != "S3A" || !s3a.DefaultKnown || s3a.Default != "S3A" {
		t.Errorf("7.10.0 should default to S3A, got %+v", s3a)
	}
	emrfs := DeriveS3Connector(S3ConnectorInput{ReleaseLabel: "emr-7.9.0"})
	if emrfs.Effective != "EMRFS" || emrfs.Default != "EMRFS" {
		t.Errorf("7.9.0 should default to EMRFS, got %+v", emrfs)
	}
	unknown := DeriveS3Connector(S3ConnectorInput{ReleaseLabel: "weird"})
	if unknown.Effective != "unknown" || unknown.DefaultKnown {
		t.Errorf("unparseable label should be unknown, got %+v", unknown)
	}
}

func TestDeriveS3Connector_OverrideWinsOverDefault(t *testing.T) {
	// A 7.10 cluster (default S3A) explicitly pinned back to EMRFS in core-site.
	v := DeriveS3Connector(S3ConnectorInput{
		ReleaseLabel: "emr-7.10.0",
		Classifications: map[string]map[string]string{
			"core-site": {"fs.s3.impl": "com.amazon.ws.emr.hadoop.fs.EmrFileSystem"},
		},
	})
	if v.Effective != "EMRFS" || v.OverrideKey == "" {
		t.Errorf("explicit EMRFS impl should override the S3A default, got %+v", v)
	}
	if v.Default != "S3A" {
		t.Errorf("the release default should still report S3A, got %q", v.Default)
	}
}

func TestDeriveS3Connector_ConsistentViewAndTable(t *testing.T) {
	// Default table name when not specified.
	v := DeriveS3Connector(S3ConnectorInput{
		ReleaseLabel:    "emr-6.10.0",
		Classifications: map[string]map[string]string{"emrfs-site": {"fs.s3.consistent": "true"}},
	})
	if !v.ConsistentView || v.ConsistentViewTable != "EmrFSMetadata" {
		t.Errorf("consistent view should default the table to EmrFSMetadata, got %+v", v)
	}
	// Custom table name honored.
	v = DeriveS3Connector(S3ConnectorInput{
		ReleaseLabel: "emr-6.10.0",
		Classifications: map[string]map[string]string{"emrfs-site": {
			"fs.s3.consistent":                    "true",
			"fs.s3.consistent.metadata.tableName": "MyMeta",
		}},
	})
	if v.ConsistentViewTable != "MyMeta" {
		t.Errorf("custom table name should be honored, got %q", v.ConsistentViewTable)
	}
	// Absent => off.
	off := DeriveS3Connector(S3ConnectorInput{ReleaseLabel: "emr-7.10.0"})
	if off.ConsistentView {
		t.Errorf("absent consistent view should read as off, got %+v", off)
	}
}

func TestDeriveS3Encryption(t *testing.T) {
	cases := []struct {
		props map[string]string
		want  string
	}{
		{map[string]string{"fs.s3.enableServerSideEncryption": "true", "fs.s3.serverSideEncryption.kms.keyId": "abcd"}, "SSE-KMS"},
		{map[string]string{"fs.s3.enableServerSideEncryption": "true"}, "SSE-S3"},
		{map[string]string{"fs.s3.cse.enabled": "true", "fs.s3.cse.kms.keyId": "k"}, "CSE-KMS"},
		{map[string]string{"fs.s3.cse.enabled": "true"}, "CSE"},
		{map[string]string{}, ""}, // unknown / set via security config — never claim "off"
	}
	for _, c := range cases {
		got := DeriveS3Connector(S3ConnectorInput{
			ReleaseLabel:    "emr-7.10.0",
			Classifications: map[string]map[string]string{"emrfs-site": c.props},
		}).Encryption
		if got != c.want {
			t.Errorf("encryption for %v = %q, want %q", c.props, got, c.want)
		}
	}
}

// The Consistent-View finding fires only when the posture is known, on, and the
// cluster is live — the §8 under-warn discipline.
func TestCheckEMRFSConsistentView(t *testing.T) {
	base := EMRCluster{ID: "j-1", Name: "prod", ARN: "arn:emr", State: "WAITING",
		ConnectorKnown: true, ConsistentView: true, ConsistentViewTable: "EmrFSMetadata",
		HasLogURI: true, HasSecurityConfig: true}
	snap := EMRSnapshot{Region: "us-east-1", Now: time.Now()}

	fire := func(c EMRCluster) bool {
		out := AnalyzeEMR(EMRSnapshot{Region: snap.Region, Now: snap.Now, Clusters: []EMRCluster{c}})
		for _, f := range out {
			if f.ID == CheckEMRFSConsistentView {
				return true
			}
		}
		return false
	}

	if !fire(base) {
		t.Error("should fire for a live cluster with consistent view on")
	}
	off := base
	off.ConsistentView = false
	if fire(off) {
		t.Error("must not fire when consistent view is off")
	}
	unknown := base
	unknown.ConnectorKnown = false
	if fire(unknown) {
		t.Error("must not fire when the connector posture is unknown (denied DescribeCluster)")
	}
	term := base
	term.State = "TERMINATED"
	if fire(term) {
		t.Error("must not fire for a terminated cluster")
	}
}

func TestCheckEMRFSConsistentView_Detail(t *testing.T) {
	out := AnalyzeEMR(EMRSnapshot{Region: "us-east-1", Now: time.Now(), Clusters: []EMRCluster{{
		ID: "j-1", Name: "prod", State: "RUNNING", HasLogURI: true, HasSecurityConfig: true,
		ConnectorKnown: true, ConsistentView: true, ConsistentViewTable: "EmrFSMetadata",
	}}})
	var f *Finding
	for i := range out {
		if out[i].ID == CheckEMRFSConsistentView {
			f = &out[i]
		}
	}
	if f == nil {
		t.Fatal("expected the EMRFS consistent-view finding")
	}
	if f.Severity != SevInfo {
		t.Errorf("severity = %v, want SevInfo", f.Severity)
	}
	if !strings.Contains(f.Detail, "EmrFSMetadata") || !strings.Contains(f.Detail, "2020") {
		t.Errorf("detail should name the table and the consistency date, got %q", f.Detail)
	}
}
