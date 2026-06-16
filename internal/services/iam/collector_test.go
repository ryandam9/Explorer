package iam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/ryandam9/aws_explorer/internal/services"
)

func TestCollector_Metadata(t *testing.T) {
	c := NewCollector()
	if c.Name() != "iam" {
		t.Errorf("Name() = %q, want %q", c.Name(), "iam")
	}
	if !c.IsGlobal() {
		t.Error("IsGlobal() = false, want true — IAM is a global service")
	}
}

func TestMapRole_BasicFields(t *testing.T) {
	c := NewCollector()
	created := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	role := types.Role{
		RoleId:     aws.String("AROA123456789"),
		RoleName:   aws.String("my-role"),
		Arn:        aws.String("arn:aws:iam::123456789012:role/my-role"),
		Path:       aws.String("/"),
		CreateDate: &created,
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.Service != "iam" {
		t.Errorf("Service = %q, want %q", res.Service, "iam")
	}
	if res.Type != "role" {
		t.Errorf("Type = %q, want %q", res.Type, "role")
	}
	if res.Region != "global" {
		t.Errorf("Region = %q, want %q", res.Region, "global")
	}
	if res.ID != "AROA123456789" {
		t.Errorf("ID = %q, want %q", res.ID, "AROA123456789")
	}
	if res.Name != "my-role" {
		t.Errorf("Name = %q, want %q", res.Name, "my-role")
	}
	if res.ARN != "arn:aws:iam::123456789012:role/my-role" {
		t.Errorf("ARN = %q", res.ARN)
	}
	if res.Summary["path"] != "/" {
		t.Errorf("Summary[path] = %q, want %q", res.Summary["path"], "/")
	}
	if res.CreatedAt == nil || !res.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt = %v, want %v", res.CreatedAt, created)
	}
}

func TestMapRole_NoDetailsAtSummaryLevel(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA000"),
		RoleName: aws.String("summary-role"),
		Arn:      aws.String("arn:aws:iam::123:role/summary-role"),
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.Details != nil {
		t.Error("expected Details to be nil at summary level")
	}
}

func TestMapRole_DetailLevel(t *testing.T) {
	c := NewCollector()
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	role := types.Role{
		RoleId:                   aws.String("AROA_DETAIL"),
		RoleName:                 aws.String("detail-role"),
		Arn:                      aws.String("arn:aws:iam::123:role/detail-role"),
		AssumeRolePolicyDocument: aws.String(policy),
		Description:              aws.String("a test role"),
		MaxSessionDuration:       aws.Int32(3600),
	}

	res := c.mapRole(role, services.DetailLevelDetailed)

	if res.Details == nil {
		t.Fatal("expected Details to be populated at detailed level")
	}
	if res.Details["assumeRolePolicyDocument"] != policy {
		t.Errorf("Details[assumeRolePolicyDocument] = %v", res.Details["assumeRolePolicyDocument"])
	}
	if res.Details["description"] != "a test role" {
		t.Errorf("Details[description] = %v", res.Details["description"])
	}
	if res.Details["maxSessionDuration"] != int32(3600) {
		t.Errorf("Details[maxSessionDuration] = %v", res.Details["maxSessionDuration"])
	}
}

func TestMapRole_RawLevelAlsoPopulatesDetails(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA_RAW"),
		RoleName: aws.String("raw-role"),
		Arn:      aws.String("arn:aws:iam::123:role/raw-role"),
	}

	res := c.mapRole(role, services.DetailLevelRaw)

	if res.Details == nil {
		t.Error("expected Details to be populated at raw level")
	}
}

func TestMapRole_NilCreateDate(t *testing.T) {
	c := NewCollector()
	role := types.Role{
		RoleId:   aws.String("AROA_NOTIME"),
		RoleName: aws.String("no-time-role"),
		Arn:      aws.String("arn:aws:iam::123:role/no-time-role"),
	}

	res := c.mapRole(role, services.DetailLevelSummary)

	if res.CreatedAt != nil {
		t.Errorf("expected nil CreatedAt, got %v", res.CreatedAt)
	}
}

func TestMapUser_Fields(t *testing.T) {
	created := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	lastUsed := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	u := types.User{
		UserId: aws.String("AID1"), UserName: aws.String("alice"),
		Arn: aws.String("arn:aws:iam::1:user/alice"), Path: aws.String("/eng/"),
		CreateDate: &created, PasswordLastUsed: &lastUsed,
	}

	res := (&Collector{}).mapUser(u, services.DetailLevelSummary)
	if res.Type != "user" || res.Name != "alice" || res.ID != "AID1" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.Region != "global" || res.Summary["path"] != "/eng/" {
		t.Errorf("region/path = %q/%q", res.Region, res.Summary["path"])
	}
	if res.Details != nil {
		t.Error("no Details expected at summary level")
	}

	det := (&Collector{}).mapUser(u, services.DetailLevelDetailed)
	if det.Details["passwordLastUsed"] != "2024-06-01 12:00:00" {
		t.Errorf("passwordLastUsed = %v", det.Details["passwordLastUsed"])
	}
}

func TestMapPolicy_AttachmentCount(t *testing.T) {
	p := types.Policy{
		PolicyId: aws.String("PID1"), PolicyName: aws.String("app-policy"),
		Arn: aws.String("arn:aws:iam::1:policy/app-policy"), Path: aws.String("/"),
		AttachmentCount: aws.Int32(3), IsAttachable: true,
		DefaultVersionId: aws.String("v2"), Description: aws.String("app perms"),
	}
	res := (&Collector{}).mapPolicy(p, services.DetailLevelDetailed)
	if res.Type != "policy" || res.Summary["attachmentCount"] != "3" {
		t.Errorf("unexpected mapping: %+v", res)
	}
	if res.Details["defaultVersionId"] != "v2" || res.Details["isAttachable"] != true {
		t.Errorf("details = %+v", res.Details)
	}
}

func TestMapInstanceProfile_Role(t *testing.T) {
	ip := types.InstanceProfile{
		InstanceProfileId: aws.String("IPID1"), InstanceProfileName: aws.String("web-profile"),
		Arn: aws.String("arn:aws:iam::1:instance-profile/web-profile"), Path: aws.String("/"),
		Roles: []types.Role{{RoleName: aws.String("web-role")}},
	}
	res := (&Collector{}).mapInstanceProfile(ip)
	if res.Type != "instance-profile" || res.Summary["role"] != "web-role" {
		t.Errorf("unexpected mapping: %+v", res)
	}
}

// fakeIAM implements iamAPI with single-page list responses and a per-family
// error (non-nil => that family's List call fails).
type fakeIAM struct {
	roleErr, userErr, groupErr, policyErr, profileErr error
}

func (f fakeIAM) ListRoles(context.Context, *iam.ListRolesInput, ...func(*iam.Options)) (*iam.ListRolesOutput, error) {
	if f.roleErr != nil {
		return nil, f.roleErr
	}
	return &iam.ListRolesOutput{Roles: []types.Role{{RoleId: aws.String("R1"), RoleName: aws.String("r1"), Arn: aws.String("arn:r1")}}}, nil
}

func (f fakeIAM) ListUsers(context.Context, *iam.ListUsersInput, ...func(*iam.Options)) (*iam.ListUsersOutput, error) {
	if f.userErr != nil {
		return nil, f.userErr
	}
	return &iam.ListUsersOutput{Users: []types.User{{UserId: aws.String("U1"), UserName: aws.String("u1"), Arn: aws.String("arn:u1")}}}, nil
}

func (f fakeIAM) ListGroups(context.Context, *iam.ListGroupsInput, ...func(*iam.Options)) (*iam.ListGroupsOutput, error) {
	if f.groupErr != nil {
		return nil, f.groupErr
	}
	return &iam.ListGroupsOutput{Groups: []types.Group{{GroupId: aws.String("G1"), GroupName: aws.String("g1"), Arn: aws.String("arn:g1")}}}, nil
}

func (f fakeIAM) ListPolicies(context.Context, *iam.ListPoliciesInput, ...func(*iam.Options)) (*iam.ListPoliciesOutput, error) {
	if f.policyErr != nil {
		return nil, f.policyErr
	}
	return &iam.ListPoliciesOutput{Policies: []types.Policy{{PolicyId: aws.String("P1"), PolicyName: aws.String("p1"), Arn: aws.String("arn:p1")}}}, nil
}

func (f fakeIAM) ListInstanceProfiles(context.Context, *iam.ListInstanceProfilesInput, ...func(*iam.Options)) (*iam.ListInstanceProfilesOutput, error) {
	if f.profileErr != nil {
		return nil, f.profileErr
	}
	return &iam.ListInstanceProfilesOutput{InstanceProfiles: []types.InstanceProfile{{InstanceProfileId: aws.String("IP1"), InstanceProfileName: aws.String("ip1"), Arn: aws.String("arn:ip1")}}}, nil
}

func TestCollect_AllFamilies(t *testing.T) {
	resources, err := (&Collector{}).collect(context.Background(), fakeIAM{}, services.CollectInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := map[string]bool{}
	for _, r := range resources {
		got[r.Type] = true
	}
	for _, want := range []string{"role", "user", "group", "policy", "instance-profile"} {
		if !got[want] {
			t.Errorf("expected a %q resource, got types %v", want, got)
		}
	}
}

func TestCollect_PartialFailureKeepsOtherFamilies(t *testing.T) {
	// Users and policies are denied; the rest must still be collected.
	api := fakeIAM{
		userErr:   errors.New("AccessDenied: ListUsers"),
		policyErr: errors.New("AccessDenied: ListPolicies"),
	}
	resources, err := (&Collector{}).collect(context.Background(), api, services.CollectInput{})
	if err == nil || !strings.Contains(err.Error(), "users") || !strings.Contains(err.Error(), "policies") {
		t.Fatalf("expected a joined error naming users and policies, got: %v", err)
	}
	got := map[string]bool{}
	for _, r := range resources {
		got[r.Type] = true
	}
	for _, want := range []string{"role", "group", "instance-profile"} {
		if !got[want] {
			t.Errorf("%q should still be collected despite user/policy failures; got %v", want, got)
		}
	}
	if got["user"] || got["policy"] {
		t.Errorf("failed families should yield no resources; got %v", got)
	}
}
