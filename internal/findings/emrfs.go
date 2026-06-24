package findings

import (
	"fmt"
	"strconv"
	"strings"
)

// EMRFS / S3A connector derivation (AXE — EMR storage connector).
//
// On Amazon EMR, the S3 URI schemes (s3://, s3a://, s3n://) are served by a
// connector: AWS's proprietary EMRFS or the open-source S3A. Which one answers
// is determined by the release label (S3A became the default in emr-7.10.0) and
// any explicit core-site override, while EMRFS Consistent View and S3 encryption
// are set in the emrfs-site classification. All of this is already present in a
// cluster's DescribeCluster response, so the verdict is a pure function over
// data we already collect — no extra API calls, fixture-testable (principle #1).

// S3ConnectorInput is the SDK-free view of a cluster's S3-connector posture:
// its release label and its configuration classifications flattened to
// classification -> property key/values.
type S3ConnectorInput struct {
	ReleaseLabel    string
	Classifications map[string]map[string]string
}

// S3Connector is the derived verdict for a cluster's S3 connector.
type S3Connector struct {
	Effective    string // "S3A" | "EMRFS" | "unknown"
	Default      string // release-based default: "S3A" | "EMRFS" | ""
	DefaultKnown bool   // false when the release label couldn't be parsed
	OverrideKey  string // the config key that pinned the connector, "" if none

	ConsistentView      bool   // EMRFS Consistent View enabled (emrfs-site fs.s3.consistent)
	ConsistentViewTable string // the DynamoDB metadata table (default EmrFSMetadata)

	Encryption string // "", "SSE-S3", "SSE-KMS", "CSE", "CSE-KMS", or the raw algorithm
}

// s3aDefaultMajor / s3aDefaultMinor is the first EMR release where S3A is the
// default connector for every S3 scheme (emr-7.10.0).
const (
	s3aDefaultMajor = 7
	s3aDefaultMinor = 10
)

// DeriveS3Connector computes the connector verdict from a cluster's release
// label and configuration classifications. Pure.
func DeriveS3Connector(in S3ConnectorInput) S3Connector {
	v := S3Connector{}

	// Default from the release label.
	if atLeast, known := emrReleaseAtLeast(in.ReleaseLabel, s3aDefaultMajor, s3aDefaultMinor); known {
		v.DefaultKnown = true
		if atLeast {
			v.Default = "S3A"
		} else {
			v.Default = "EMRFS"
		}
	}

	get := func(cls, key string) (string, bool) {
		if m, ok := in.Classifications[cls]; ok {
			val, ok2 := m[key]
			return val, ok2
		}
		return "", false
	}

	// Explicit override in core-site wins over the release default.
	if impl, ok := get("core-site", "fs.s3.impl"); ok {
		switch {
		case strings.Contains(impl, "S3AFileSystem"):
			v.OverrideKey, v.Effective = "core-site fs.s3.impl", "S3A"
		case strings.Contains(impl, "EmrFileSystem"):
			v.OverrideKey, v.Effective = "core-site fs.s3.impl", "EMRFS"
		}
	}
	if v.Effective == "" {
		if v.DefaultKnown {
			v.Effective = v.Default
		} else {
			v.Effective = "unknown"
		}
	}

	// EMRFS Consistent View (emrfs-site). Absent => off (it is off by default and
	// DescribeCluster returns only user-supplied properties).
	if cv, ok := get("emrfs-site", "fs.s3.consistent"); ok && isTrue(cv) {
		v.ConsistentView = true
		if t, ok := get("emrfs-site", "fs.s3.consistent.metadata.tableName"); ok && strings.TrimSpace(t) != "" {
			v.ConsistentViewTable = t
		} else {
			v.ConsistentViewTable = "EmrFSMetadata"
		}
	}

	v.Encryption = deriveS3Encryption(get)
	return v
}

// deriveS3Encryption reports the S3 encryption configured in emrfs-site, or ""
// when none is set there. Absence is NOT "no encryption": it can be configured
// via an EMR security configuration (a separate API this does not read), so
// callers should render "" as unknown/—, never as "off" (§6a/§8).
func deriveS3Encryption(get func(string, string) (string, bool)) string {
	if cse, ok := get("emrfs-site", "fs.s3.cse.enabled"); ok && isTrue(cse) {
		if k, ok := get("emrfs-site", "fs.s3.cse.kms.keyId"); ok && strings.TrimSpace(k) != "" {
			return "CSE-KMS"
		}
		return "CSE"
	}
	if sse, ok := get("emrfs-site", "fs.s3.enableServerSideEncryption"); ok && isTrue(sse) {
		if k, ok := get("emrfs-site", "fs.s3.serverSideEncryption.kms.keyId"); ok && strings.TrimSpace(k) != "" {
			return "SSE-KMS"
		}
		if algo, ok := get("emrfs-site", "fs.s3.serverSideEncryptionAlgorithm"); ok && strings.TrimSpace(algo) != "" {
			return strings.TrimSpace(algo)
		}
		return "SSE-S3"
	}
	return ""
}

// emrReleaseAtLeast reports whether an EMR release label (e.g. "emr-7.10.0") is
// at least major.minor. known is false when the label can't be parsed, so the
// caller can leave the default unknown rather than guess (§8).
func emrReleaseAtLeast(label string, wantMajor, wantMinor int) (atLeast, known bool) {
	major, minor, ok := parseEMRRelease(label)
	if !ok {
		return false, false
	}
	if major != wantMajor {
		return major > wantMajor, true
	}
	return minor >= wantMinor, true
}

// parseEMRRelease extracts the major and minor version from an EMR release
// label like "emr-7.10.0" or "emr-6.15.0".
func parseEMRRelease(label string) (major, minor int, ok bool) {
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(label)), "emr-")
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	ma, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	mi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return ma, mi, true
}

func isTrue(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }

// checkEMRFSConsistentView flags a cluster running EMRFS Consistent View, which
// has been obsolete since Amazon S3 gained strong read-after-write consistency
// (December 2020). It is tri-state safe: it stays silent unless the connector
// posture was actually read (ConnectorKnown) and Consistent View is on.
func checkEMRFSConsistentView(snap EMRSnapshot, c EMRCluster, out *[]Finding) {
	if !c.ConnectorKnown || !c.ConsistentView || isTerminatedState(c.State) {
		return
	}
	res := c.Name
	if res == "" {
		res = c.ID
	}
	table := c.ConsistentViewTable
	if table == "" {
		table = "EmrFSMetadata"
	}
	*out = append(*out, Finding{
		ID: CheckEMRFSConsistentView, Severity: SevInfo, Service: "emr", Region: snap.Region,
		Resource: res, ARN: c.ARN,
		Title: "EMRFS Consistent View is enabled (obsolete)",
		Detail: fmt.Sprintf("The cluster has EMRFS Consistent View on, which tracks S3 metadata in a DynamoDB table (%s). "+
			"Amazon S3 has had strong read-after-write consistency since December 2020, so Consistent View no longer adds correctness — only DynamoDB cost and a throttling risk.", table),
		Fix: "Disable it: set fs.s3.consistent=false in the emrfs-site configuration, then delete the now-unused DynamoDB metadata table.",
	})
}
