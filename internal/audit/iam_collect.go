package audit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"golang.org/x/sync/errgroup"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/model"
)

const (
	maxRoleChecks   = 200
	maxPolicyChecks = 200

	// iamFetchConcurrencyCap bounds how many per-role / per-policy IAM calls run
	// at once. The role and policy enrichment loops each fire one AWS call per
	// item (up to maxRoleChecks / maxPolicyChecks), which run sequentially would
	// routinely blow the per-family timeout on large accounts. A modest cap
	// keeps the fan-out fast without tripping IAM's low request rate.
	iamFetchConcurrencyCap = 10

	// Credential report generation is asynchronous; poll briefly and degrade
	// (no user findings) if it doesn't complete in time.
	credReportPollInterval = 2 * time.Second
	credReportMaxPolls     = 8
)

// collectIAMAccount gathers the account-global IAM hygiene snapshot. Runs
// once per audit (in the first region's pass); findings carry Region
// "global". Best-effort: each source degrades independently.
func collectIAMAccount(ctx context.Context, cfg aws.Config, maxConcurrency int, perCallTimeout time.Duration) (findings.IAMSnapshot, []model.ExploreError) {
	snap := findings.IAMSnapshot{Now: time.Now().UTC()}
	rec := &errRecorder{region: "global"}
	client := awsiam.NewFromConfig(cfg)

	conc := maxConcurrency
	if conc <= 0 {
		conc = 1
	}
	if conc > iamFetchConcurrencyCap {
		conc = iamFetchConcurrencyCap
	}

	collectCredentialReport(ctx, client, &snap, rec, perCallTimeout)
	collectRolesIAM(ctx, client, &snap, rec, conc, perCallTimeout)
	collectPoliciesIAM(ctx, client, &snap, rec, conc, perCallTimeout)

	return snap, dedupeIAMErrors(rec.errs, perCallTimeout)
}

// dedupeIAMErrors keeps the best-effort error list readable. The role/policy
// loops fan out one AWS call per item, so a single expired deadline surfaces as
// dozens of identical "context deadline exceeded" errors — enough to fill the
// errors overlay (see issue #154). This collapses every cancellation/deadline
// error into one actionable summary and drops exact duplicates of the rest.
func dedupeIAMErrors(errs []model.ExploreError, timeout time.Duration) []model.ExploreError {
	if len(errs) == 0 {
		return errs
	}
	out := make([]model.ExploreError, 0, len(errs))
	seen := make(map[string]bool, len(errs))
	timedOut := 0
	for _, e := range errs {
		if isDeadlineMessage(e.Message) {
			timedOut++
			continue
		}
		key := e.Service + "\x00" + e.Code + "\x00" + e.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	if timedOut > 0 {
		msg := fmt.Sprintf("%d IAM API call(s) did not finish in time; some role/policy checks were skipped", timedOut)
		if timeout > 0 {
			msg += fmt.Sprintf(" (per-scan timeout %s — raise app.timeoutSeconds to scan large accounts fully)", timeout)
		}
		out = append(out, model.ExploreError{
			Service: "iam", Region: "global", Code: "Timeout", Message: msg,
		})
	}
	return out
}

// isDeadlineMessage reports whether a recorded error message describes a context
// cancellation or deadline. errRecorder stores the formatted string, so this
// matches on text (the raw error.Is check happens at the call site via addErr).
func isDeadlineMessage(msg string) bool {
	return strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "context canceled")
}

// isDeadline reports whether an error is a context cancellation/deadline,
// including SDK-wrapped forms.
func isDeadline(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
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

func collectRolesIAM(ctx context.Context, client *awsiam.Client, snap *findings.IAMSnapshot, rec *errRecorder, conc int, timeout time.Duration) {
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

	// RoleLastUsed comes only from GetRole, one call per role. Fan these out
	// with bounded concurrency: serially they routinely exceed the per-family
	// timeout on accounts with many roles, leaving the snapshot empty and the
	// errors overlay flooded (issue #154).
	out := make([]findings.IAMRole, len(roles))
	errs := make([]error, len(roles))
	g := new(errgroup.Group)
	g.SetLimit(conc)
	for i := range roles {
		i, r := i, roles[i]
		role := findings.IAMRole{
			Name:        aws.ToString(r.RoleName),
			ARN:         aws.ToString(r.Arn),
			ServiceRole: strings.HasPrefix(aws.ToString(r.Path), "/aws-service-role/"),
			Created:     aws.ToTime(r.CreateDate),
			TrustPolicy: findings.DecodePolicyDocument(aws.ToString(r.AssumeRolePolicyDocument)),
		}
		out[i] = role
		// Service-linked roles are AWS-managed and skipped by the usage check,
		// so don't spend a GetRole call on them.
		if role.ServiceRole {
			continue
		}
		g.Go(func() error {
			res, err := client.GetRole(ctx, &awsiam.GetRoleInput{RoleName: r.RoleName})
			if err != nil {
				errs[i] = err
				return nil
			}
			if res.Role != nil {
				out[i].LastUsedKnown = true
				if lu := res.Role.RoleLastUsed; lu != nil {
					out[i].LastUsed = aws.ToTime(lu.LastUsedDate)
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	snap.Roles = append(snap.Roles, out...)
	recordErrs(rec, errs)
}

func collectPoliciesIAM(ctx context.Context, client *awsiam.Client, snap *findings.IAMSnapshot, rec *errRecorder, conc int, timeout time.Duration) {
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

	// Each policy needs GetPolicyVersion (its document) and, when attached,
	// ListEntitiesForPolicy (its users). Fan out per policy with bounded
	// concurrency for the same reason as the role loop.
	out := make([]findings.IAMPolicy, len(pols))
	errs := make([]error, len(pols))
	g := new(errgroup.Group)
	g.SetLimit(conc)
	for i := range pols {
		i, p := i, pols[i]
		out[i] = findings.IAMPolicy{
			Name: aws.ToString(p.PolicyName),
			ARN:  aws.ToString(p.Arn),
		}
		g.Go(func() error {
			if p.DefaultVersionId != nil {
				ver, err := client.GetPolicyVersion(ctx, &awsiam.GetPolicyVersionInput{
					PolicyArn: p.Arn, VersionId: p.DefaultVersionId,
				})
				if err != nil {
					errs[i] = err
				} else if ver.PolicyVersion != nil {
					out[i].Document = findings.DecodePolicyDocument(aws.ToString(ver.PolicyVersion.Document))
				}
			}
			// Direct user attachments only matter for attached policies.
			// Paginate: a policy attached to more than one page of users would
			// otherwise have its attached-user list silently truncated.
			if p.AttachmentCount != nil && *p.AttachmentCount > 0 {
				ep := awsiam.NewListEntitiesForPolicyPaginator(client, &awsiam.ListEntitiesForPolicyInput{
					PolicyArn:    p.Arn,
					EntityFilter: iamtypes.EntityTypeUser,
				})
				for ep.HasMorePages() {
					ents, err := ep.NextPage(ctx)
					if err != nil {
						if errs[i] == nil {
							errs[i] = err
						}
						break
					}
					for _, u := range ents.PolicyUsers {
						out[i].AttachedUsers = append(out[i].AttachedUsers, aws.ToString(u.UserName))
					}
				}
			}
			return nil
		})
	}
	_ = g.Wait()

	snap.Policies = append(snap.Policies, out...)
	recordErrs(rec, errs)
}

// recordErrs folds a parallel loop's per-item errors back into the recorder.
// Deadline/cancellation errors are left for dedupeIAMErrors to collapse into a
// single summary; everything else is recorded normally.
func recordErrs(rec *errRecorder, errs []error) {
	for _, err := range errs {
		if err == nil {
			continue
		}
		if isDeadline(err) {
			// Record as a deadline message so dedupeIAMErrors can collapse it,
			// rather than emitting one line per timed-out call.
			rec.errs = append(rec.errs, model.ExploreError{
				Service: "iam", Region: "global", Code: "Timeout",
				Message: "context deadline exceeded",
			})
			continue
		}
		rec.record("iam", err)
	}
}
