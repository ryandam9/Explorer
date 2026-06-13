package findings

import (
	"strings"
	"testing"
	"time"
)

var iamNow = time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)

func daysAgo(d int) time.Time { return iamNow.Add(-time.Duration(d) * 24 * time.Hour) }

func TestAnalyzeIAM_CleanSnapshotNoFindings(t *testing.T) {
	snap := IAMSnapshot{
		Now: iamNow,
		Users: []IAMUser{{
			Name: "alice", PasswordEnabled: true, MFAActive: true,
			Keys: []IAMAccessKey{{Slot: 1, Active: true, LastRotated: daysAgo(30), LastUsed: daysAgo(1)}},
		}},
		Roles: []IAMRole{{
			Name: "app", LastUsedKnown: true, LastUsed: daysAgo(5), Created: daysAgo(400),
			TrustPolicy: `{"Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`,
		}},
		Policies: []IAMPolicy{{
			Name: "scoped", Document: `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::b/*"}]}`,
		}},
	}
	if fs := AnalyzeIAM(snap); len(fs) != 0 {
		t.Errorf("clean snapshot produced findings: %+v", fs)
	}
}

func TestAnalyzeIAM_Keys(t *testing.T) {
	snap := IAMSnapshot{Now: iamNow, Users: []IAMUser{
		{Name: "old-key", Keys: []IAMAccessKey{{Slot: 1, Active: true, LastRotated: daysAgo(120), LastUsed: daysAgo(2)}}},
		{Name: "idle-key", Keys: []IAMAccessKey{{Slot: 1, Active: true, LastRotated: daysAgo(120), LastUsed: daysAgo(100)}}},
		{Name: "never-used", Keys: []IAMAccessKey{{Slot: 2, Active: true, LastRotated: daysAgo(120)}}},
		{Name: "inactive-old", Keys: []IAMAccessKey{{Slot: 1, Active: false, LastRotated: daysAgo(400)}}},
		{Name: "fresh-unused", Keys: []IAMAccessKey{{Slot: 1, Active: true, LastRotated: daysAgo(10)}}},
	}}
	fs := AnalyzeIAM(snap)
	byRes := map[string]string{}
	for _, f := range fs {
		byRes[f.Resource] = f.ID
	}
	if byRes["old-key"] != CheckOldAccessKey {
		t.Errorf("old-key → %v, want %s", byRes["old-key"], CheckOldAccessKey)
	}
	if byRes["idle-key"] != CheckUnusedAccessKey {
		t.Errorf("idle-key → %v, want %s", byRes["idle-key"], CheckUnusedAccessKey)
	}
	if byRes["never-used"] != CheckUnusedAccessKey {
		t.Errorf("never-used → %v, want %s", byRes["never-used"], CheckUnusedAccessKey)
	}
	if _, ok := byRes["inactive-old"]; ok {
		t.Error("inactive key must not be flagged")
	}
	if _, ok := byRes["fresh-unused"]; ok {
		t.Error("fresh never-used key must not be flagged")
	}
}

func TestAnalyzeIAM_RootAndMFA(t *testing.T) {
	snap := IAMSnapshot{Now: iamNow, Users: []IAMUser{
		{Name: "<root_account>", IsRoot: true, PasswordEnabled: true,
			Keys: []IAMAccessKey{{Slot: 1, Active: true, LastRotated: daysAgo(500)}}},
		{Name: "bob", PasswordEnabled: true, MFAActive: false},
		{Name: "svc", PasswordEnabled: false, MFAActive: false}, // API-only user: no MFA finding
	}}
	got := ids(AnalyzeIAM(snap))
	if got[CheckRootAccessKey] != 1 {
		t.Errorf("root key findings = %d, want 1", got[CheckRootAccessKey])
	}
	if got[CheckUserNoMFA] != 1 {
		t.Errorf("no-MFA findings = %d, want 1 (got %v)", got[CheckUserNoMFA], got)
	}
	// Root's stale key must not double-report under the user key checks.
	if got[CheckOldAccessKey] != 0 && got[CheckUnusedAccessKey] != 0 {
		t.Errorf("root key also hit user key checks: %v", got)
	}
}

func TestAnalyzeIAM_Roles(t *testing.T) {
	openTrust := `{"Statement":[{"Effect":"Allow","Principal":{"AWS":"*"},"Action":"sts:AssumeRole"}]}`
	snap := IAMSnapshot{Now: iamNow, Roles: []IAMRole{
		{Name: "open", LastUsedKnown: true, LastUsed: daysAgo(1), Created: daysAgo(400), TrustPolicy: openTrust},
		{Name: "stale", LastUsedKnown: true, LastUsed: daysAgo(200), Created: daysAgo(400)},
		{Name: "never", LastUsedKnown: true, Created: daysAgo(400)},
		{Name: "new", LastUsedKnown: true, Created: daysAgo(10)},
		{Name: "unknown", LastUsedKnown: false, Created: daysAgo(400)},
		{Name: "svc-role", ServiceRole: true, LastUsedKnown: true, Created: daysAgo(400)},
	}}
	fs := AnalyzeIAM(snap)
	perRole := map[string][]string{}
	for _, f := range fs {
		perRole[f.Resource] = append(perRole[f.Resource], f.ID)
	}
	if len(perRole["open"]) != 1 || perRole["open"][0] != CheckOpenTrustPolicy {
		t.Errorf("open → %v", perRole["open"])
	}
	if len(perRole["stale"]) != 1 || perRole["stale"][0] != CheckUnusedRole {
		t.Errorf("stale → %v", perRole["stale"])
	}
	if len(perRole["never"]) != 1 || perRole["never"][0] != CheckUnusedRole {
		t.Errorf("never → %v", perRole["never"])
	}
	for _, quiet := range []string{"new", "unknown", "svc-role"} {
		if len(perRole[quiet]) != 0 {
			t.Errorf("%s must not be flagged: %v", quiet, perRole[quiet])
		}
	}
}

func TestAnalyzeIAM_Policies(t *testing.T) {
	snap := IAMSnapshot{Now: iamNow, Policies: []IAMPolicy{
		{Name: "admin", Document: `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`},
		{Name: "direct", Document: `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`,
			AttachedUsers: []string{"alice", "bob"}},
	}}
	fs := AnalyzeIAM(snap)
	got := ids(fs)
	if got[CheckWildcardPolicy] != 1 || got[CheckUserAttachedPolicy] != 1 {
		t.Errorf("findings = %v", got)
	}
	for _, f := range fs {
		if f.ID == CheckUserAttachedPolicy && !strings.Contains(f.Detail, "alice, bob") {
			t.Errorf("attached-users detail = %q", f.Detail)
		}
	}
}

func TestPolicyGrantsFullAdmin(t *testing.T) {
	cases := []struct {
		name   string
		policy string
		want   bool
	}{
		{"star/star", `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"*"}]}`, true},
		{"star-colon-star", `{"Statement":[{"Effect":"Allow","Action":"*:*","Resource":"*"}]}`, true},
		{"action list", `{"Statement":[{"Effect":"Allow","Action":["s3:Get*","*"],"Resource":["*"]}]}`, true},
		{"single statement object", `{"Statement":{"Effect":"Allow","Action":"*","Resource":"*"}}`, true},
		{"deny", `{"Statement":[{"Effect":"Deny","Action":"*","Resource":"*"}]}`, false},
		{"scoped resource", `{"Statement":[{"Effect":"Allow","Action":"*","Resource":"arn:aws:s3:::b/*"}]}`, false},
		{"scoped action", `{"Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`, false},
		{"empty", "", false},
		{"garbage", "nope", false},
	}
	for _, c := range cases {
		if got := PolicyGrantsFullAdmin(c.policy); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

const credReportFixture = `user,arn,user_creation_time,password_enabled,password_last_used,password_last_changed,password_next_rotation,mfa_active,access_key_1_active,access_key_1_last_rotated,access_key_1_last_used_date,access_key_2_active,access_key_2_last_rotated,access_key_2_last_used_date
<root_account>,arn:aws:iam::123456789012:root,2020-01-01T00:00:00+00:00,not_supported,2026-06-01T10:00:00+00:00,not_supported,not_supported,true,true,2021-03-04T05:00:00+00:00,N/A,false,N/A,N/A
alice,arn:aws:iam::123456789012:user/alice,2022-05-01T00:00:00+00:00,true,2026-06-10T09:00:00+00:00,2026-01-01T00:00:00+00:00,N/A,true,true,2026-05-01T00:00:00+00:00,2026-06-12T08:00:00+00:00,false,N/A,N/A
bob,arn:aws:iam::123456789012:user/bob,2023-01-01T00:00:00+00:00,true,no_information,2023-01-01T00:00:00+00:00,N/A,false,false,N/A,N/A,true,2024-01-01T00:00:00+00:00,2024-02-01T00:00:00+00:00
`

func TestParseCredentialReport(t *testing.T) {
	users, err := ParseCredentialReport([]byte(credReportFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 3 {
		t.Fatalf("users = %d, want 3", len(users))
	}
	root := users[0]
	if !root.IsRoot || !root.Keys[0].Active || !root.MFAActive {
		t.Errorf("root parsed wrong: %+v", root)
	}
	alice := users[1]
	if !alice.PasswordEnabled || !alice.MFAActive || !alice.Keys[0].Active {
		t.Errorf("alice parsed wrong: %+v", alice)
	}
	if alice.Keys[0].LastUsed.IsZero() || alice.Keys[1].Active {
		t.Errorf("alice keys parsed wrong: %+v", alice.Keys)
	}
	bob := users[2]
	if bob.MFAActive || !bob.Keys[1].Active || bob.Keys[1].LastUsed.IsZero() {
		t.Errorf("bob parsed wrong: %+v", bob)
	}
	if !bob.Keys[0].LastRotated.IsZero() {
		t.Errorf("N/A date must parse as zero: %+v", bob.Keys[0])
	}
}

func TestParseCredentialReport_EndToEnd(t *testing.T) {
	users, err := ParseCredentialReport([]byte(credReportFixture))
	if err != nil {
		t.Fatal(err)
	}
	fs := AnalyzeIAM(IAMSnapshot{Now: iamNow, Users: users})
	got := ids(fs)
	// root: active key → IAM-ROOT-001. bob: console, no MFA → IAM-USER-001;
	// key 2 active, unused since 2024 → IAM-KEY-002. alice: clean.
	if got[CheckRootAccessKey] != 1 || got[CheckUserNoMFA] != 1 || got[CheckUnusedAccessKey] != 1 {
		t.Errorf("findings = %v", got)
	}
	if len(fs) != 3 {
		t.Errorf("total findings = %d, want 3: %+v", len(fs), fs)
	}
}

func TestDecodePolicyDocument(t *testing.T) {
	enc := "%7B%22Statement%22%3A%5B%5D%7D"
	if got := DecodePolicyDocument(enc); got != `{"Statement":[]}` {
		t.Errorf("decoded = %q", got)
	}
	plain := `{"Statement":[]}`
	if got := DecodePolicyDocument(plain); got != plain {
		t.Errorf("plain passthrough = %q", got)
	}
}
