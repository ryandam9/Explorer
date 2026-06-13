package auth

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// CallerIdentity is the subset of sts:GetCallerIdentity output the app uses to
// confirm who the active credentials belong to.
type CallerIdentity struct {
	Account string
	ARN     string
	UserID  string
}

// VerifyCallerIdentity confirms that the credentials in awscfg are present and
// currently valid by calling sts:GetCallerIdentity — the canonical "can I
// authenticate?" check. Every caller is allowed to make this call regardless
// of their IAM policy, so a failure means the credentials themselves are
// missing, expired or otherwise invalid rather than under-privileged.
//
// STS is global; when awscfg carries no region the call is pinned to
// us-east-1 so the endpoint always resolves (callers that only ever browse a
// snapshot, or that never set a region, would otherwise fail with an
// endpoint-resolution error instead of a clear auth verdict).
func VerifyCallerIdentity(ctx context.Context, awscfg aws.Config) (CallerIdentity, error) {
	if awscfg.Region == "" {
		awscfg.Region = "us-east-1"
	}
	out, err := sts.NewFromConfig(awscfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return CallerIdentity{}, err
	}
	return CallerIdentity{
		Account: aws.ToString(out.Account),
		ARN:     aws.ToString(out.Arn),
		UserID:  aws.ToString(out.UserId),
	}, nil
}

// Verify builds an aws.Config for the given auth settings and region and
// confirms the resulting credentials can call AWS, returning the resolved
// caller identity on success. Both failure modes collapse into one error here:
// a config-build failure (e.g. an expired SSO session the SDK cannot refresh,
// or missing static credentials) and a rejected GetCallerIdentity call — so a
// caller gets a single yes/no answer to "can this user call AWS right now?".
func Verify(ctx context.Context, cfg *config.AWSConfig, region string) (CallerIdentity, error) {
	awscfg, err := BuildAWSConfig(ctx, cfg, region)
	if err != nil {
		return CallerIdentity{}, err
	}
	return VerifyCallerIdentity(ctx, awscfg)
}
