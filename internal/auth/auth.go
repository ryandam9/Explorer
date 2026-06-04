package auth

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/user/aws_explorer/internal/config"
)

// BuildAWSConfig creates an aws.Config for the specified region using the
// authentication method described in cfg. Supported methods:
//
//   - "auto" (default) – AWS SDK default credential chain
//   - "profile"        – named profile from ~/.aws/config or ~/.aws/credentials
//   - "env"            – AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY env vars only
//   - "static"         – credentials embedded in config (aws.static.*)
//   - "sts"            – AssumeRole via STS (aws.sts.roleArn required)
func BuildAWSConfig(ctx context.Context, cfg *config.AWSConfig, region string) (aws.Config, error) {
	method := cfg.AuthMethod
	if method == "" {
		method = "auto"
	}

	switch method {
	case "auto":
		return buildAuto(ctx, cfg, region)
	case "profile":
		return buildProfile(ctx, cfg, region)
	case "env":
		return buildEnv(ctx, region)
	case "static":
		return buildStatic(ctx, cfg, region)
	case "sts":
		return buildSTS(ctx, cfg, region)
	default:
		return aws.Config{}, fmt.Errorf("unknown authMethod %q — valid values: auto, profile, env, static, sts", method)
	}
}

func regionOpts(region string) []func(*awsconfig.LoadOptions) error {
	if region == "" {
		return nil
	}
	return []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
}

// buildAuto uses the AWS SDK default credential chain with an optional profile.
func buildAuto(ctx context.Context, cfg *config.AWSConfig, region string) (aws.Config, error) {
	opts := regionOpts(region)
	if cfg.Profile != "" && cfg.Profile != "default" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// buildProfile requires an explicit profile name.
func buildProfile(ctx context.Context, cfg *config.AWSConfig, region string) (aws.Config, error) {
	if cfg.Profile == "" {
		return aws.Config{}, fmt.Errorf("authMethod \"profile\" requires aws.profile to be set")
	}
	opts := regionOpts(region)
	opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// buildEnv forces credentials from environment variables only.
func buildEnv(ctx context.Context, region string) (aws.Config, error) {
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if accessKeyID == "" || secretKey == "" {
		return aws.Config{}, fmt.Errorf("authMethod \"env\" requires AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY to be set")
	}
	opts := regionOpts(region)
	opts = append(opts, awsconfig.WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(accessKeyID, secretKey, os.Getenv("AWS_SESSION_TOKEN")),
	))
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// buildStatic uses plaintext credentials from config.
func buildStatic(ctx context.Context, cfg *config.AWSConfig, region string) (aws.Config, error) {
	if cfg.Static.AccessKeyID == "" || cfg.Static.SecretAccessKey == "" {
		return aws.Config{}, fmt.Errorf("authMethod \"static\" requires aws.static.accessKeyId and aws.static.secretAccessKey")
	}
	opts := regionOpts(region)
	opts = append(opts, awsconfig.WithCredentialsProvider(
		credentials.NewStaticCredentialsProvider(
			cfg.Static.AccessKeyID,
			cfg.Static.SecretAccessKey,
			cfg.Static.SessionToken,
		),
	))
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// buildSTS assumes the configured IAM role via STS, using the auto/profile
// credential chain as the caller identity.
func buildSTS(ctx context.Context, cfg *config.AWSConfig, region string) (aws.Config, error) {
	if cfg.STS.RoleARN == "" {
		return aws.Config{}, fmt.Errorf("authMethod \"sts\" requires aws.sts.roleArn to be set")
	}

	// Bootstrap credentials used to call sts:AssumeRole.
	baseCfg, err := buildAuto(ctx, cfg, region)
	if err != nil {
		return aws.Config{}, fmt.Errorf("building base credentials for STS: %w", err)
	}

	stsClient := sts.NewFromConfig(baseCfg)
	provider := stscreds.NewAssumeRoleProvider(stsClient, cfg.STS.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		if cfg.STS.SessionName != "" {
			o.RoleSessionName = cfg.STS.SessionName
		}
		if cfg.STS.ExternalID != "" {
			o.ExternalID = aws.String(cfg.STS.ExternalID)
		}
		if cfg.STS.MFASerial != "" {
			o.SerialNumber = aws.String(cfg.STS.MFASerial)
			o.TokenProvider = stscreds.StdinTokenProvider
		}
		if cfg.STS.Duration > 0 {
			o.Duration = time.Duration(cfg.STS.Duration) * time.Second
		}
	})

	baseCfg.Credentials = aws.NewCredentialsCache(provider)
	return baseCfg, nil
}
