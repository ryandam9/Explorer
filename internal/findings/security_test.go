package findings

import (
	"testing"
	"time"
)

func boolp(b bool) *bool { return &b }

func ids(fs []Finding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.ID]++
	}
	return m
}

func TestAnalyzeSecurity_CleanSnapshotNoFindings(t *testing.T) {
	snap := SecuritySnapshot{
		Region: "us-east-1", Now: time.Now(),
		S3Scanned: true,
		Buckets: []SecBucket{{
			Name: "good", PolicyPublic: boolp(false), PABAllOn: boolp(true), Encrypted: boolp(true),
		}},
		Volumes:              []SecVolume{{ID: "vol-1", Encrypted: true}},
		EBSDefaultEncryption: boolp(true),
		Instances:            []SecInstance{{ID: "i-1", State: "running", HTTPTokens: "required"}},
		SecurityGroups:       []SecGroup{{ID: "sg-1"}},
		DBInstances:          []SecDBInstance{{ID: "db-1", StorageEncrypted: true}},
		Functions:            []SecFunction{{Name: "fn"}},
		Queues:               []SecQueue{{Name: "q", Policy: ""}},
		Topics:               []SecTopic{{Name: "t"}},
		Alarms:               []SecAlarm{{Name: "ok", StateUpdated: time.Now().Add(-time.Hour)}},
	}
	if fs := AnalyzeSecurity(snap); len(fs) != 0 {
		t.Errorf("clean snapshot produced findings: %+v", fs)
	}
}

func TestAnalyzeSecurity_S3(t *testing.T) {
	snap := SecuritySnapshot{Region: "us-east-1", S3Scanned: true, Buckets: []SecBucket{
		{Name: "open", Region: "eu-west-1", PolicyPublic: boolp(true), PABAllOn: boolp(false), Encrypted: boolp(false)},
		{Name: "unknown"}, // all nil: a denied call must not fire checks
	}}
	got := ids(AnalyzeSecurity(snap))
	for _, want := range []string{CheckS3Public, CheckS3PABOff, CheckS3EncryptionOff} {
		if got[want] != 1 {
			t.Errorf("%s fired %d times, want 1 (got %v)", want, got[want], got)
		}
	}
	if len(got) != 3 {
		t.Errorf("unexpected extra findings: %v", got)
	}
}

func TestAnalyzeSecurity_EBSAndSnapshots(t *testing.T) {
	snap := SecuritySnapshot{
		Region:               "us-east-1",
		Volumes:              []SecVolume{{ID: "vol-plain"}, {ID: "vol-enc", Encrypted: true}},
		EBSDefaultEncryption: boolp(false),
		PublicEBSSnapshots:   []string{"snap-pub"},
		PublicRDSSnapshots:   []string{"rds-snap-pub"},
	}
	got := ids(AnalyzeSecurity(snap))
	for _, want := range []string{CheckEBSUnencrypted, CheckEBSDefaultEncOff, CheckPublicEBSSnapshot, CheckPublicRDSSnapshot} {
		if got[want] != 1 {
			t.Errorf("%s fired %d times, want 1", want, got[want])
		}
	}
}

func TestAnalyzeSecurity_IMDS(t *testing.T) {
	snap := SecuritySnapshot{Region: "us-east-1", Instances: []SecInstance{
		{ID: "i-v1", State: "running", HTTPTokens: "optional"},
		{ID: "i-v2", State: "running", HTTPTokens: "required"},
		{ID: "i-dead", State: "terminated", HTTPTokens: "optional"},
		{ID: "i-noimds", State: "running", HTTPTokens: "optional", HTTPEndpoint: "disabled"},
	}}
	fs := AnalyzeSecurity(snap)
	if len(fs) != 1 || fs[0].ID != CheckIMDSv1 || fs[0].Resource != "i-v1" {
		t.Errorf("findings = %+v", fs)
	}
}

func TestAnalyzeSecurity_SecurityGroups(t *testing.T) {
	snap := SecuritySnapshot{Region: "us-east-1", SecurityGroups: []SecGroup{
		{ID: "sg-ssh", Name: "bastion", Rules: []SecSGRule{
			{Protocol: "tcp", FromPort: 22, ToPort: 22, Source: "0.0.0.0/0"},
			{Protocol: "tcp", FromPort: 22, ToPort: 22, Source: "::/0"}, // dedup with v4
		}},
		{ID: "sg-all", Rules: []SecSGRule{{Protocol: "-1", FromPort: -1, ToPort: -1, Source: "0.0.0.0/0"}}},
		{ID: "sg-https", Rules: []SecSGRule{{Protocol: "tcp", FromPort: 443, ToPort: 443, Source: "0.0.0.0/0"}}},
		{ID: "sg-udp", Rules: []SecSGRule{{Protocol: "udp", FromPort: 22, ToPort: 22, Source: "0.0.0.0/0"}}},
	}}
	fs := AnalyzeSecurity(snap)
	got := ids(fs)
	if got[CheckSGOpenPort] == 0 {
		t.Fatalf("no SG findings: %v", got)
	}
	perSG := map[string]int{}
	for _, f := range fs {
		perSG[f.Resource]++
	}
	if perSG["sg-ssh"] != 1 {
		t.Errorf("sg-ssh findings = %d, want 1 (v4/v6 deduped)", perSG["sg-ssh"])
	}
	if perSG["sg-all"] != len(sensitivePorts) {
		t.Errorf("sg-all findings = %d, want %d (all sensitive ports)", perSG["sg-all"], len(sensitivePorts))
	}
	if perSG["sg-https"] != 0 || perSG["sg-udp"] != 0 {
		t.Errorf("https/udp must not fire: %v", perSG)
	}
}

func TestAnalyzeSecurity_RDSLambda(t *testing.T) {
	snap := SecuritySnapshot{
		Region:      "us-east-1",
		DBInstances: []SecDBInstance{{ID: "db-open", PublicAccessible: true, StorageEncrypted: false}},
		Functions:   []SecFunction{{Name: "open-fn", URLNoAuth: true}, {Name: "safe-fn"}},
	}
	got := ids(AnalyzeSecurity(snap))
	for _, want := range []string{CheckRDSPublic, CheckRDSUnencrypted, CheckLambdaURLNoAuth} {
		if got[want] != 1 {
			t.Errorf("%s fired %d times, want 1", want, got[want])
		}
	}
}

func TestAnalyzeSecurity_Alarms(t *testing.T) {
	now := time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC)
	snap := SecuritySnapshot{Region: "us-east-1", Now: now, Alarms: []SecAlarm{
		{Name: "stuck", StateUpdated: now.Add(-10 * 24 * time.Hour)},
		{Name: "recent", StateUpdated: now.Add(-2 * 24 * time.Hour)},
		{Name: "unknown"}, // zero time: skip
	}}
	fs := AnalyzeSecurity(snap)
	if len(fs) != 1 || fs[0].Resource != "stuck" {
		t.Errorf("findings = %+v", fs)
	}
}

func TestPolicyAllowsEveryone(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		want   bool
	}{
		{"empty", "", false},
		{"star principal", `{"Statement":[{"Effect":"Allow","Principal":"*","Action":"sqs:*"}]}`, true},
		{"aws star", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"sns:Publish"}]}`, true},
		{"aws star in list", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":["arn:aws:iam::1:root","*"]}}]}`, true},
		{"single statement object", `{"Statement":{"Effect":"Allow","Principal":"*"}}`, true},
		{"deny star is fine", `{"Statement":[{"Effect":"Deny","Principal":"*"}]}`, false},
		{"condition exempts", `{"Statement":[{"Effect":"Allow","Principal":"*","Condition":{"ArnEquals":{"aws:SourceArn":"arn:x"}}}]}`, false},
		{"scoped account", `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123:root"}}]}`, false},
		{"service principal", `{"Statement":[{"Effect":"Allow","Principal":{"Service":"events.amazonaws.com"}}]}`, false},
		{"garbage", "not json", false},
	}
	for _, c := range cases {
		if got := PolicyAllowsEveryone(c.policy); got != c.want {
			t.Errorf("%s: PolicyAllowsEveryone = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSecurityChecksAreRegistered(t *testing.T) {
	for _, id := range []string{
		CheckS3Public, CheckS3PABOff, CheckS3EncryptionOff,
		CheckEBSUnencrypted, CheckEBSDefaultEncOff, CheckPublicEBSSnapshot,
		CheckRDSPublic, CheckRDSUnencrypted, CheckPublicRDSSnapshot,
		CheckIMDSv1, CheckSGOpenPort, CheckLambdaURLNoAuth,
		CheckSQSOpenPolicy, CheckSNSOpenPolicy, CheckAlarmNoData,
	} {
		if _, ok := CheckByID(id); !ok {
			t.Errorf("check %s not registered", id)
		}
	}
}
