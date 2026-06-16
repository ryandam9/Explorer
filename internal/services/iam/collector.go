package iam

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/services"
)

// iamAPI is the subset of the IAM client used by the collector. Splitting it
// out lets each resource family be exercised with a fake client in tests.
type iamAPI interface {
	iam.ListRolesAPIClient
	iam.ListUsersAPIClient
	iam.ListGroupsAPIClient
	iam.ListPoliciesAPIClient
	iam.ListInstanceProfilesAPIClient
}

// Collector implements the services.Collector interface for IAM.
type Collector struct{}

// NewCollector returns a new IAM Collector.
func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Name() string {
	return "iam"
}

func (c *Collector) IsGlobal() bool {
	return true
}

func (c *Collector) Collect(ctx context.Context, input services.CollectInput) ([]model.Resource, error) {
	return c.collect(ctx, iam.NewFromConfig(input.AWSConfig), input)
}

// collect gathers every IAM identity/resource family independently: a failure
// in one (e.g. a denied ListUsers) degrades only that family — the others are
// still queried and already-collected resources are kept. Errors are joined so
// the engine records a partial result rather than aborting IAM entirely.
func (c *Collector) collect(ctx context.Context, api iamAPI, input services.CollectInput) ([]model.Resource, error) {
	var resources []model.Resource
	var errs []error

	collectors := []func(context.Context, iamAPI, services.CollectInput, []model.Resource) ([]model.Resource, error){
		c.collectRoles,
		c.collectUsers,
		c.collectGroups,
		c.collectPolicies,
		c.collectInstanceProfiles,
	}
	for _, collect := range collectors {
		var err error
		if resources, err = collect(ctx, api, input, resources); err != nil {
			errs = append(errs, err)
		}
	}

	return resources, errors.Join(errs...)
}

func (c *Collector) collectRoles(ctx context.Context, api iamAPI, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := iam.NewListRolesPaginator(api, &iam.ListRolesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to list IAM roles: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Roles))
		for _, role := range page.Roles {
			batch = append(batch, c.mapRole(role, input.DetailLevel))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectUsers(ctx context.Context, api iamAPI, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := iam.NewListUsersPaginator(api, &iam.ListUsersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to list IAM users: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Users))
		for _, user := range page.Users {
			batch = append(batch, c.mapUser(user, input.DetailLevel))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectGroups(ctx context.Context, api iamAPI, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := iam.NewListGroupsPaginator(api, &iam.ListGroupsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to list IAM groups: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Groups))
		for _, group := range page.Groups {
			batch = append(batch, c.mapGroup(group))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

// collectPolicies lists only customer-managed policies (Scope=Local): the
// hundreds of AWS-managed policies are not user-owned and would swamp the
// inventory.
func (c *Collector) collectPolicies(ctx context.Context, api iamAPI, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := iam.NewListPoliciesPaginator(api, &iam.ListPoliciesInput{Scope: types.PolicyScopeTypeLocal})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to list IAM policies: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.Policies))
		for _, policy := range page.Policies {
			batch = append(batch, c.mapPolicy(policy, input.DetailLevel))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) collectInstanceProfiles(ctx context.Context, api iamAPI, input services.CollectInput, acc []model.Resource) ([]model.Resource, error) {
	p := iam.NewListInstanceProfilesPaginator(api, &iam.ListInstanceProfilesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return acc, fmt.Errorf("failed to list IAM instance profiles: %w", err)
		}
		batch := make([]model.Resource, 0, len(page.InstanceProfiles))
		for _, profile := range page.InstanceProfiles {
			batch = append(batch, c.mapInstanceProfile(profile))
		}
		acc = input.EmitOrAppend(acc, batch)
	}
	return acc, nil
}

func (c *Collector) mapRole(role types.Role, detail services.DetailLevel) model.Resource {
	id := aws.ToString(role.RoleId)
	name := aws.ToString(role.RoleName)
	arn := aws.ToString(role.Arn)

	res := model.Resource{
		Service: "iam",
		Type:    "role",
		Region:  "global",
		ID:      id,
		Name:    name,
		ARN:     arn,
		Summary: map[string]string{
			"path": aws.ToString(role.Path),
		},
	}

	if role.CreateDate != nil {
		res.CreatedAt = role.CreateDate
	}

	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"assumeRolePolicyDocument": aws.ToString(role.AssumeRolePolicyDocument),
			"description":              aws.ToString(role.Description),
			"maxSessionDuration":       aws.ToInt32(role.MaxSessionDuration),
		}
	}

	return res
}

func (c *Collector) mapUser(user types.User, detail services.DetailLevel) model.Resource {
	res := model.Resource{
		Service: "iam",
		Type:    "user",
		Region:  "global",
		ID:      aws.ToString(user.UserId),
		Name:    aws.ToString(user.UserName),
		ARN:     aws.ToString(user.Arn),
		Summary: map[string]string{
			"path": aws.ToString(user.Path),
		},
	}
	if user.CreateDate != nil {
		res.CreatedAt = user.CreateDate
	}
	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		// passwordLastUsed distinguishes a console user from a
		// programmatic-only user; nil means "never used / no console password".
		lastUsed := ""
		if user.PasswordLastUsed != nil {
			lastUsed = user.PasswordLastUsed.UTC().Format("2006-01-02 15:04:05")
		}
		res.Details = map[string]any{
			"passwordLastUsed": lastUsed,
		}
	}
	return res
}

func (c *Collector) mapGroup(group types.Group) model.Resource {
	res := model.Resource{
		Service: "iam",
		Type:    "group",
		Region:  "global",
		ID:      aws.ToString(group.GroupId),
		Name:    aws.ToString(group.GroupName),
		ARN:     aws.ToString(group.Arn),
		Summary: map[string]string{
			"path": aws.ToString(group.Path),
		},
	}
	if group.CreateDate != nil {
		res.CreatedAt = group.CreateDate
	}
	return res
}

func (c *Collector) mapPolicy(policy types.Policy, detail services.DetailLevel) model.Resource {
	res := model.Resource{
		Service: "iam",
		Type:    "policy",
		Region:  "global",
		ID:      aws.ToString(policy.PolicyId),
		Name:    aws.ToString(policy.PolicyName),
		ARN:     aws.ToString(policy.Arn),
		Summary: map[string]string{
			"path":            aws.ToString(policy.Path),
			"attachmentCount": strconv.Itoa(int(aws.ToInt32(policy.AttachmentCount))),
		},
	}
	if policy.CreateDate != nil {
		res.CreatedAt = policy.CreateDate
	}
	if detail == services.DetailLevelDetailed || detail == services.DetailLevelRaw {
		res.Details = map[string]any{
			"description":      aws.ToString(policy.Description),
			"defaultVersionId": aws.ToString(policy.DefaultVersionId),
			"isAttachable":     policy.IsAttachable,
		}
	}
	return res
}

func (c *Collector) mapInstanceProfile(profile types.InstanceProfile) model.Resource {
	res := model.Resource{
		Service: "iam",
		Type:    "instance-profile",
		Region:  "global",
		ID:      aws.ToString(profile.InstanceProfileId),
		Name:    aws.ToString(profile.InstanceProfileName),
		ARN:     aws.ToString(profile.Arn),
		Summary: map[string]string{
			"path": aws.ToString(profile.Path),
		},
	}
	// An instance profile references at most one role; surface it since the
	// role is what actually grants the EC2 instance its permissions.
	if len(profile.Roles) > 0 {
		res.Summary["role"] = aws.ToString(profile.Roles[0].RoleName)
	}
	if profile.CreateDate != nil {
		res.CreatedAt = profile.CreateDate
	}
	return res
}
