package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/ryandam9/aws_explorer/internal/config"
)

// clearAWSEnv blanks the ambient AWS environment so tests behave the same on
// developer machines and CI runners that have credentials configured.
func clearAWSEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN",
		"AWS_PROFILE", "AWS_REGION", "AWS_DEFAULT_REGION",
		"AWS_RETRY_MODE", "AWS_MAX_ATTEMPTS",
	} {
		t.Setenv(key, "")
	}
}

func TestBuildAWSConfig_UnknownMethod(t *testing.T) {
	clearAWSEnv(t)
	_, err := BuildAWSConfig(context.Background(), &config.AWSConfig{AuthMethod: "magic"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), `unknown authMethod "magic"`) {
		t.Fatalf("expected unknown authMethod error, got %v", err)
	}
}

func TestBuildAWSConfig_ProfileRequiresName(t *testing.T) {
	clearAWSEnv(t)
	_, err := BuildAWSConfig(context.Background(), &config.AWSConfig{AuthMethod: "profile"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "requires aws.profile") {
		t.Fatalf("expected missing profile error, got %v", err)
	}
}

func TestBuildAWSConfig_EnvRequiresVariables(t *testing.T) {
	clearAWSEnv(t)
	_, err := BuildAWSConfig(context.Background(), &config.AWSConfig{AuthMethod: "env"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("expected missing env vars error, got %v", err)
	}
}

func TestBuildAWSConfig_StaticRequiresCredentials(t *testing.T) {
	clearAWSEnv(t)
	_, err := BuildAWSConfig(context.Background(), &config.AWSConfig{AuthMethod: "static"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "aws.static.accessKeyId") {
		t.Fatalf("expected missing static credentials error, got %v", err)
	}
}

func TestBuildAWSConfig_STSRequiresRoleARN(t *testing.T) {
	clearAWSEnv(t)
	_, err := BuildAWSConfig(context.Background(), &config.AWSConfig{AuthMethod: "sts"}, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), "aws.sts.roleArn") {
		t.Fatalf("expected missing roleArn error, got %v", err)
	}
}

func TestBuildAWSConfig_StaticCredentialsResolve(t *testing.T) {
	clearAWSEnv(t)
	cfg := &config.AWSConfig{
		AuthMethod: "static",
		Static: config.StaticCredentials{
			AccessKeyID:     "AKIDEXAMPLE",
			SecretAccessKey: "secret",
			SessionToken:    "token",
		},
	}
	awscfg, err := BuildAWSConfig(context.Background(), cfg, "eu-west-1")
	if err != nil {
		t.Fatalf("BuildAWSConfig: %v", err)
	}
	if awscfg.Region != "eu-west-1" {
		t.Errorf("Region = %q, want %q", awscfg.Region, "eu-west-1")
	}
	creds, err := awscfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve credentials: %v", err)
	}
	if creds.AccessKeyID != "AKIDEXAMPLE" || creds.SecretAccessKey != "secret" || creds.SessionToken != "token" {
		t.Errorf("unexpected credentials: %+v", creds)
	}
}

func TestBuildAWSConfig_RetrySettingsApplied(t *testing.T) {
	clearAWSEnv(t)
	cfg := &config.AWSConfig{
		AuthMethod: "static",
		Static:     config.StaticCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret"},
		Retry:      config.RetryConfig{MaxAttempts: 7, Mode: "adaptive"},
	}
	awscfg, err := BuildAWSConfig(context.Background(), cfg, "us-east-1")
	if err != nil {
		t.Fatalf("BuildAWSConfig: %v", err)
	}
	if awscfg.RetryMaxAttempts != 7 {
		t.Errorf("RetryMaxAttempts = %d, want 7", awscfg.RetryMaxAttempts)
	}
	if awscfg.RetryMode != aws.RetryModeAdaptive {
		t.Errorf("RetryMode = %q, want %q", awscfg.RetryMode, aws.RetryModeAdaptive)
	}
}

func TestBuildAWSConfig_RetryModeCaseInsensitive(t *testing.T) {
	clearAWSEnv(t)
	cfg := &config.AWSConfig{
		AuthMethod: "static",
		Static:     config.StaticCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret"},
		Retry:      config.RetryConfig{Mode: "Standard"},
	}
	awscfg, err := BuildAWSConfig(context.Background(), cfg, "us-east-1")
	if err != nil {
		t.Fatalf("BuildAWSConfig: %v", err)
	}
	if awscfg.RetryMode != aws.RetryModeStandard {
		t.Errorf("RetryMode = %q, want %q", awscfg.RetryMode, aws.RetryModeStandard)
	}
}

func TestBuildAWSConfig_InvalidRetryMode(t *testing.T) {
	clearAWSEnv(t)
	cfg := &config.AWSConfig{
		AuthMethod: "static",
		Static:     config.StaticCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret"},
		Retry:      config.RetryConfig{Mode: "aggressive"},
	}
	_, err := BuildAWSConfig(context.Background(), cfg, "us-east-1")
	if err == nil || !strings.Contains(err.Error(), `unknown aws.retry.mode "aggressive"`) {
		t.Fatalf("expected invalid retry mode error, got %v", err)
	}
}

func TestBuildAWSConfig_DefaultRetryUntouched(t *testing.T) {
	clearAWSEnv(t)
	cfg := &config.AWSConfig{
		AuthMethod: "static",
		Static:     config.StaticCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret"},
	}
	awscfg, err := BuildAWSConfig(context.Background(), cfg, "us-east-1")
	if err != nil {
		t.Fatalf("BuildAWSConfig: %v", err)
	}
	// Zero config must not override the SDK defaults explicitly.
	if awscfg.RetryMaxAttempts != 0 && awscfg.RetryMaxAttempts != 3 {
		t.Errorf("RetryMaxAttempts = %d, want SDK default (0 or 3)", awscfg.RetryMaxAttempts)
	}
}
