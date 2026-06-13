package audit

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxRoleChecks   = 200
	maxPolicyChecks = 200

	// Credential report generation is asynchronous; poll briefly and degrade
	// (no user findings) if it doesn't complete in time.
	credReportPollInterval = 2 * time.Second
	credReportMaxPolls     = 8
)

// collectIAMAccount gathers the account-global IAM hygiene snapshot. Runs
// once per audit (in the first region's pass); findings carry Region
// "global". Best-effort: each source degrades independently.
func collectIAMAccount(ctx context.Context, cfg aws.Config, perCallTimeout time.Duration) (findings.IAMSnapshot, []model.ExploreError) {
	snap := findings.IAMSnapshot{Now: time.Now().UTC()}
	rec := &errRecorder{region: "global"}
	client := awsiam.NewFromConfig(cfg)

	collectCredentialReport(ctx, client, &snap, rec, perCallTimeout)
	collectRolesIAM(ctx, client, &snap, rec, perCallTimeout)
	collectPoliciesIAM(ctx, client, &snap, rec, perCallTimeout)

	return snap, rec.errs
}

func collectCredentialReport(ctx context.Context, client *awsiam.Client, snap *findings.IAMSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	// Kick off generation and poll until COMPLETE; an already-fresh report
	// returns COMPLETE immediately.
	ready := false
	for i := 0; i < credReportMaxPolls; i++ {
		out, err := client.GenerateCredentialReport(ctx, &awsiam.GenerateCredentialReportInput{})
		if err != nil {
			rec.record("iam", err)
			return
		}
		if out.State == iamtypes.ReportStateTypeComplete {
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			rec.record("iam", ctx.Err())
			return
		case <-time.After(credReportPollInterval):
		}
	}
	if !ready {
		rec.errs = append(rec.errs, model.ExploreError{
			Service: "iam", Region: "global", Code: "Timeout",
			Message: "credential report did not finish generating; user/key checks skipped this run",
		})
		return
	}

	report, err := client.GetCredentialReport(ctx, &awsiam.GetCredentialReportInput{})
	if err != nil {
		rec.record("iam", err)
		return
	}
	users, err := findings.ParseCredentialReport(report.Content)
	if err != nil {
		rec.record("iam", err)
		return
	}
	snap.Users = users
}

func collectRolesIAM(ctx context.Context, client *awsiam.Client, snap *findings.IAMSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	var roles []iamtypes.Role
	pager := awsiam.NewListRolesPaginator(client, &awsiam.ListRolesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			return
		}
		roles = append(roles, page.Roles...)
		if len(roles) >= maxRoleChecks {
			rec.recordTruncation("iam", "roles", maxRoleChecks)
			roles = roles[:maxRoleChecks]
			break
		}
	}

	for _, r := range roles {
		role := findings.IAMRole{
			Name:        aws.ToString(r.RoleName),
			ARN:         aws.ToString(r.Arn),
			ServiceRole: strings.HasPrefix(aws.ToString(r.Path), "/aws-service-role/"),
			Created:     aws.ToTime(r.CreateDate),
			TrustPolicy: findings.DecodePolicyDocument(aws.ToString(r.AssumeRolePolicyDocument)),
		}
		// RoleLastUsed comes only from GetRole. Service-linked roles are
		// AWS-managed and skipped by the usage check, so don't spend a call.
		if !role.ServiceRole {
			out, err := client.GetRole(ctx, &awsiam.GetRoleInput{RoleName: r.RoleName})
			if err != nil {
				rec.record("iam", err)
			} else if out.Role != nil {
				role.LastUsedKnown = true
				if lu := out.Role.RoleLastUsed; lu != nil {
					role.LastUsed = aws.ToTime(lu.LastUsedDate)
				}
			}
		}
		snap.Roles = append(snap.Roles, role)
	}
}

func collectPoliciesIAM(ctx context.Context, client *awsiam.Client, snap *findings.IAMSnapshot, rec *errRecorder, timeout time.Duration) {
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()

	var pols []iamtypes.Policy
	pager := awsiam.NewListPoliciesPaginator(client, &awsiam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal, // customer-managed only
	})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			rec.record("iam", err)
			return
		}
		pols = append(pols, page.Policies...)
		if len(pols) >= maxPolicyChecks {
			rec.recordTruncation("iam", "customer policies", maxPolicyChecks)
			pols = pols[:maxPolicyChecks]
			break
		}
	}

	for _, p := range pols {
		policy := findings.IAMPolicy{
			Name: aws.ToString(p.PolicyName),
			ARN:  aws.ToString(p.Arn),
		}
		if p.DefaultVersionId != nil {
			ver, err := client.GetPolicyVersion(ctx, &awsiam.GetPolicyVersionInput{
				PolicyArn: p.Arn, VersionId: p.DefaultVersionId,
			})
			if err != nil {
				rec.record("iam", err)
			} else if ver.PolicyVersion != nil {
				policy.Document = findings.DecodePolicyDocument(aws.ToString(ver.PolicyVersion.Document))
			}
		}
		// Direct user attachments only matter for attached policies.
		if p.AttachmentCount != nil && *p.AttachmentCount > 0 {
			ents, err := client.ListEntitiesForPolicy(ctx, &awsiam.ListEntitiesForPolicyInput{
				PolicyArn:    p.Arn,
				EntityFilter: iamtypes.EntityTypeUser,
			})
			if err != nil {
				rec.record("iam", err)
			} else {
				for _, u := range ents.PolicyUsers {
					policy.AttachedUsers = append(policy.AttachedUsers, aws.ToString(u.UserName))
				}
			}
		}
		snap.Policies = append(snap.Policies, policy)
	}
}
