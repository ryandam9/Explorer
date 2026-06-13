package findings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Security check IDs (stable; see README "The checks").
const (
	CheckS3Public          = "SEC-S3-001"
	CheckS3PABOff          = "SEC-S3-002"
	CheckS3EncryptionOff   = "SEC-S3-003"
	CheckEBSUnencrypted    = "SEC-EBS-001"
	CheckEBSDefaultEncOff  = "SEC-EBS-002"
	CheckPublicEBSSnapshot = "SEC-SNAP-001"
	CheckRDSPublic         = "SEC-RDS-001"
	CheckRDSUnencrypted    = "SEC-RDS-002"
	CheckPublicRDSSnapshot = "SEC-RDS-003"
	CheckIMDSv1            = "SEC-EC2-001"
	CheckSGOpenPort        = "SEC-SG-001"
	CheckLambdaURLNoAuth   = "SEC-LAMBDA-001"
	CheckSQSOpenPolicy     = "SEC-SQS-001"
	CheckSNSOpenPolicy     = "SEC-SNS-001"
	CheckAlarmNoData       = "SEC-CW-001"
)

// alarmNoDataAge is how long an alarm may sit in INSUFFICIENT_DATA before it
// counts as broken monitoring rather than a deployment in progress.
const alarmNoDataAge = 7 * 24 * time.Hour

// sensitivePorts are the ports whose exposure to 0.0.0.0/0 pages people.
// Mirrors the VPC linter's exposure list, applied account-wide.
var sensitivePorts = map[int32]string{
	22:    "SSH",
	3389:  "RDP",
	3306:  "MySQL",
	5432:  "PostgreSQL",
	1433:  "SQL Server",
	27017: "MongoDB",
	6379:  "Redis",
	9200:  "Elasticsearch",
	11211: "memcached",
}

// SecuritySnapshot is the per-region input to AnalyzeSecurity. Tri-state
// facts use *bool: nil means "could not determine" (the call was denied or
// failed), and no check fires on unknowns — the linter under-warns rather
// than mis-warns.
type SecuritySnapshot struct {
	Region string
	Now    time.Time

	// S3 is account-global; Buckets is populated by one designated region
	// pass only (S3Scanned reports whether this snapshot was it).
	S3Scanned bool
	Buckets   []SecBucket

	Volumes              []SecVolume
	EBSDefaultEncryption *bool    // account/region default-encryption setting
	PublicEBSSnapshots   []string // snapshot IDs restorable by "all"

	Instances      []SecInstance
	SecurityGroups []SecGroup

	DBInstances        []SecDBInstance
	PublicRDSSnapshots []string

	Functions []SecFunction
	Queues    []SecQueue
	Topics    []SecTopic
	Alarms    []SecAlarm
}

// SecBucket is one S3 bucket's security posture.
type SecBucket struct {
	Name         string
	Region       string
	PolicyPublic *bool // GetBucketPolicyStatus; nil = unknown
	PABAllOn     *bool // all four Public Access Block flags on; false includes "no PAB configured"
	Encrypted    *bool // default encryption configured
}

// SecVolume is an EBS volume's encryption state.
type SecVolume struct {
	ID        string
	Encrypted bool
}

// SecInstance is an EC2 instance's metadata-service posture.
type SecInstance struct {
	ID           string
	Name         string
	State        string
	HTTPTokens   string // "required" (IMDSv2 only) or "optional" (IMDSv1 allowed)
	HTTPEndpoint string // "enabled" / "disabled"
}

// SecGroup is a security group reduced to its world-open inbound rules.
type SecGroup struct {
	ID    string
	Name  string
	Rules []SecSGRule // only rules whose source is 0.0.0.0/0 or ::/0
}

// SecSGRule is one world-open inbound rule. FromPort/ToPort of -1 mean all
// ports (protocol "-1" or unset range).
type SecSGRule struct {
	Protocol string // "tcp", "udp", "-1", …
	FromPort int32
	ToPort   int32
	Source   string // "0.0.0.0/0" or "::/0"
}

// SecDBInstance is an RDS instance's exposure/encryption posture.
type SecDBInstance struct {
	ID               string
	PublicAccessible bool
	StorageEncrypted bool
}

// SecFunction is a Lambda function's URL config posture.
type SecFunction struct {
	Name      string
	URLNoAuth bool // a function URL exists with AuthType NONE
}

// SecQueue is an SQS queue's resource policy posture.
type SecQueue struct {
	Name   string
	Policy string // raw policy JSON, "" when none
}

// SecTopic is an SNS topic's resource policy posture.
type SecTopic struct {
	ARN    string
	Name   string
	Policy string
}

// SecAlarm is a CloudWatch alarm stuck without data.
type SecAlarm struct {
	Name         string
	StateUpdated time.Time
}

// AnalyzeSecurity runs every security check over the snapshot. Pure.
func AnalyzeSecurity(snap SecuritySnapshot) []Finding {
	var out []Finding
	checkBuckets(snap, &out)
	checkSecVolumes(snap, &out)
	checkPublicSnapshots(snap, &out)
	checkInstancesIMDS(snap, &out)
	checkSecGroups(snap, &out)
	checkDBInstances(snap, &out)
	checkLambdaURLs(snap, &out)
	checkQueuePolicies(snap, &out)
	checkTopicPolicies(snap, &out)
	checkAlarms(snap, &out)
	return out
}

func checkBuckets(snap SecuritySnapshot, out *[]Finding) {
	for _, b := range snap.Buckets {
		region := b.Region
		if region == "" {
			region = "global"
		}
		if b.PolicyPublic != nil && *b.PolicyPublic {
			*out = append(*out, Finding{
				ID: CheckS3Public, Severity: SevCritical, Service: "s3", Region: region,
				Resource: b.Name,
				Title:    "S3 bucket is public",
				Detail:   "The bucket policy status reports this bucket as public.",
				Fix:      "Remove the public statements from the bucket policy, or enable Public Access Block.",
			})
		}
		if b.PABAllOn != nil && !*b.PABAllOn {
			*out = append(*out, Finding{
				ID: CheckS3PABOff, Severity: SevCritical, Service: "s3", Region: region,
				Resource: b.Name,
				Title:    "S3 Public Access Block is not fully enabled",
				Detail:   "One or more of the four Public Access Block settings is off (or no configuration exists), so a policy or ACL change can expose the bucket.",
				Fix:      "Enable all four Public Access Block settings on the bucket (and consider the account-level block).",
			})
		}
		if b.Encrypted != nil && !*b.Encrypted {
			*out = append(*out, Finding{
				ID: CheckS3EncryptionOff, Severity: SevWarning, Service: "s3", Region: region,
				Resource: b.Name,
				Title:    "S3 bucket has no default encryption configuration",
				Detail:   "Objects uploaded without an encryption header are stored unencrypted.",
				Fix:      "Enable default encryption (SSE-S3 or SSE-KMS) on the bucket.",
			})
		}
	}
}

func checkSecVolumes(snap SecuritySnapshot, out *[]Finding) {
	for _, v := range snap.Volumes {
		if !v.Encrypted {
			*out = append(*out, Finding{
				ID: CheckEBSUnencrypted, Severity: SevWarning, Service: "ec2", Region: snap.Region,
				Resource: v.ID,
				Title:    "EBS volume is not encrypted",
				Detail:   "Snapshots and copies of this volume inherit the unencrypted state.",
				Fix:      "Snapshot the volume, copy the snapshot with encryption, and recreate the volume from it.",
			})
		}
	}
	if snap.EBSDefaultEncryption != nil && !*snap.EBSDefaultEncryption {
		*out = append(*out, Finding{
			ID: CheckEBSDefaultEncOff, Severity: SevWarning, Service: "ec2", Region: snap.Region,
			Resource: "account",
			Title:    "EBS default encryption is off in this region",
			Detail:   "New volumes created without an explicit setting are unencrypted.",
			Fix:      "Enable EBS encryption by default for the region (EnableEbsEncryptionByDefault).",
		})
	}
}

func checkPublicSnapshots(snap SecuritySnapshot, out *[]Finding) {
	for _, id := range snap.PublicEBSSnapshots {
		*out = append(*out, Finding{
			ID: CheckPublicEBSSnapshot, Severity: SevCritical, Service: "ec2", Region: snap.Region,
			Resource: id,
			Title:    "EBS snapshot is shared publicly",
			Detail:   "Anyone with an AWS account can copy this snapshot and read its data.",
			Fix:      "Remove the 'all' create-volume permission (ModifySnapshotAttribute).",
		})
	}
	for _, id := range snap.PublicRDSSnapshots {
		*out = append(*out, Finding{
			ID: CheckPublicRDSSnapshot, Severity: SevCritical, Service: "rds", Region: snap.Region,
			Resource: id,
			Title:    "RDS snapshot is shared publicly",
			Detail:   "Anyone with an AWS account can restore this snapshot and read the database.",
			Fix:      "Remove 'all' from the snapshot's restore attribute (ModifyDBSnapshotAttribute).",
		})
	}
}

func checkInstancesIMDS(snap SecuritySnapshot, out *[]Finding) {
	for _, i := range snap.Instances {
		if strings.EqualFold(i.State, "terminated") || strings.EqualFold(i.HTTPEndpoint, "disabled") {
			continue
		}
		if !strings.EqualFold(i.HTTPTokens, "required") {
			res := i.ID
			if i.Name != "" {
				res += " (" + i.Name + ")"
			}
			*out = append(*out, Finding{
				ID: CheckIMDSv1, Severity: SevWarning, Service: "ec2", Region: snap.Region,
				Resource: res,
				Title:    "EC2 instance allows IMDSv1",
				Detail:   "HttpTokens is not 'required', so the unauthenticated v1 metadata service — the classic SSRF credential-theft vector — is available.",
				Fix:      "Require IMDSv2: aws ec2 modify-instance-metadata-options --http-tokens required.",
			})
		}
	}
}

func checkSecGroups(snap SecuritySnapshot, out *[]Finding) {
	for _, sg := range snap.SecurityGroups {
		reported := map[string]bool{} // dedupe per sg+port across v4/v6 rules
		for _, r := range sg.Rules {
			for _, port := range openSensitivePorts(r) {
				key := fmt.Sprintf("%d", port)
				if reported[key] {
					continue
				}
				reported[key] = true
				label := sg.ID
				if sg.Name != "" && sg.Name != sg.ID {
					label += " (" + sg.Name + ")"
				}
				*out = append(*out, Finding{
					ID: CheckSGOpenPort, Severity: SevCritical, Service: "ec2", Region: snap.Region,
					Resource: sg.ID,
					Title:    fmt.Sprintf("Security group opens %s (port %d) to the internet", sensitivePorts[port], port),
					Detail:   fmt.Sprintf("%s allows inbound %s from %s.", label, portLabel(port), r.Source),
					Fix:      "Restrict the source to specific CIDRs or a security group instead of the whole internet.",
				})
			}
		}
	}
}

// openSensitivePorts returns the sensitive ports a world-open rule exposes,
// sorted. A protocol of "-1" (all traffic) or an unset port range exposes
// every sensitive port.
func openSensitivePorts(r SecSGRule) []int32 {
	proto := strings.ToLower(r.Protocol)
	if proto != "tcp" && proto != "-1" && proto != "" {
		return nil
	}
	var out []int32
	for port := range sensitivePorts {
		if proto == "-1" || (r.FromPort < 0 && r.ToPort < 0) ||
			(r.FromPort <= port && port <= r.ToPort) {
			out = append(out, port)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func portLabel(port int32) string {
	return fmt.Sprintf("%s/%d", sensitivePorts[port], port)
}

func checkDBInstances(snap SecuritySnapshot, out *[]Finding) {
	for _, db := range snap.DBInstances {
		if db.PublicAccessible {
			*out = append(*out, Finding{
				ID: CheckRDSPublic, Severity: SevCritical, Service: "rds", Region: snap.Region,
				Resource: db.ID,
				Title:    "RDS instance is publicly accessible",
				Detail:   "PubliclyAccessible is on: the instance resolves to a public IP and is reachable from the internet (subject to its security groups).",
				Fix:      "Disable public accessibility, or strictly limit the security group sources.",
			})
		}
		if !db.StorageEncrypted {
			*out = append(*out, Finding{
				ID: CheckRDSUnencrypted, Severity: SevWarning, Service: "rds", Region: snap.Region,
				Resource: db.ID,
				Title:    "RDS storage is not encrypted",
				Detail:   "Storage encryption cannot be enabled in place.",
				Fix:      "Snapshot, copy the snapshot with encryption, and restore.",
			})
		}
	}
}

func checkLambdaURLs(snap SecuritySnapshot, out *[]Finding) {
	for _, fn := range snap.Functions {
		if fn.URLNoAuth {
			*out = append(*out, Finding{
				ID: CheckLambdaURLNoAuth, Severity: SevCritical, Service: "lambda", Region: snap.Region,
				Resource: fn.Name,
				Title:    "Lambda function URL requires no auth",
				Detail:   "A function URL with AuthType NONE invokes the function for anyone on the internet.",
				Fix:      "Switch the URL to AuthType AWS_IAM, or delete it and front the function with API Gateway.",
			})
		}
	}
}

func checkQueuePolicies(snap SecuritySnapshot, out *[]Finding) {
	for _, q := range snap.Queues {
		if PolicyAllowsEveryone(q.Policy) {
			*out = append(*out, Finding{
				ID: CheckSQSOpenPolicy, Severity: SevCritical, Service: "sqs", Region: snap.Region,
				Resource: q.Name,
				Title:    "SQS queue policy allows everyone",
				Detail:   `The queue policy has an Allow statement with Principal "*" and no Condition.`,
				Fix:      "Scope the principal to specific accounts/roles, or add a restricting condition (e.g. aws:SourceArn).",
			})
		}
	}
}

func checkTopicPolicies(snap SecuritySnapshot, out *[]Finding) {
	for _, t := range snap.Topics {
		if PolicyAllowsEveryone(t.Policy) {
			res := t.Name
			if res == "" {
				res = t.ARN
			}
			*out = append(*out, Finding{
				ID: CheckSNSOpenPolicy, Severity: SevCritical, Service: "sns", Region: snap.Region,
				Resource: res,
				Title:    "SNS topic policy allows everyone",
				Detail:   `The topic policy has an Allow statement with Principal "*" and no Condition.`,
				Fix:      "Scope the principal to specific accounts/roles, or add a restricting condition (e.g. aws:SourceArn).",
			})
		}
	}
}

func checkAlarms(snap SecuritySnapshot, out *[]Finding) {
	for _, a := range snap.Alarms {
		if a.StateUpdated.IsZero() || snap.Now.Sub(a.StateUpdated) < alarmNoDataAge {
			continue
		}
		days := int(snap.Now.Sub(a.StateUpdated).Hours() / 24)
		*out = append(*out, Finding{
			ID: CheckAlarmNoData, Severity: SevInfo, Service: "cloudwatch", Region: snap.Region,
			Resource: a.Name,
			Title:    "Alarm stuck in INSUFFICIENT_DATA (broken monitoring)",
			Detail:   fmt.Sprintf("The alarm has had no data for %d days — its metric, dimension, or source probably no longer exists.", days),
			Fix:      "Fix the alarm's metric/dimensions, or delete the alarm if the resource is gone.",
		})
	}
}

// ---------------------------------------------------------------------------
// Resource-policy analysis
// ---------------------------------------------------------------------------

// iamPolicyDoc is the subset of an IAM policy document the open-policy check
// needs. Statement accepts both a single object and an array.
type iamPolicyDoc struct {
	Statement jsonStatements `json:"Statement"`
}

type jsonStatements []iamStatement

func (s *jsonStatements) UnmarshalJSON(b []byte) error {
	var arr []iamStatement
	if err := json.Unmarshal(b, &arr); err == nil {
		*s = arr
		return nil
	}
	var one iamStatement
	if err := json.Unmarshal(b, &one); err != nil {
		return err
	}
	*s = []iamStatement{one}
	return nil
}

type iamStatement struct {
	Effect    string          `json:"Effect"`
	Principal json.RawMessage `json:"Principal"`
	Condition json.RawMessage `json:"Condition"`
}

// PolicyAllowsEveryone reports whether the policy document contains an Allow
// statement whose principal is everyone ("*" or {"AWS": "*"}) with no
// Condition. Pure string/JSON work — fixture-testable.
func PolicyAllowsEveryone(policyJSON string) bool {
	if strings.TrimSpace(policyJSON) == "" {
		return false
	}
	var doc iamPolicyDoc
	if json.Unmarshal([]byte(policyJSON), &doc) != nil {
		return false
	}
	for _, st := range doc.Statement {
		if !strings.EqualFold(st.Effect, "Allow") {
			continue
		}
		if len(st.Condition) > 0 && string(st.Condition) != "null" && string(st.Condition) != "{}" {
			continue
		}
		if principalIsEveryone(st.Principal) {
			return true
		}
	}
	return false
}

func principalIsEveryone(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s == "*"
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	aws, ok := m["AWS"]
	if !ok {
		return false
	}
	if json.Unmarshal(aws, &s) == nil {
		return s == "*"
	}
	var list []string
	if json.Unmarshal(aws, &list) == nil {
		for _, p := range list {
			if p == "*" {
				return true
			}
		}
	}
	return false
}
