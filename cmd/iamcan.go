package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/iamsim"
	"github.com/ryandam9/aws_explorer/internal/trail"
)

var iamCanCmd = &cobra.Command{
	Use:   "can <principal-arn> <action> [resource-arn]",
	Short: `Simulate IAM policy: "can X do Y on Z?"`,
	Long: `Can runs iam:SimulatePrincipalPolicy for a principal (role or user ARN) and
renders the verdict step by step — allowed, implicit deny (no policy allows
it), or explicit deny (a policy forbids it; removing an allow elsewhere will
not help) — naming the matched policy statements and whether a permissions
boundary is the limiting factor.

The action accepts a comma-separated list ("s3:GetObject,s3:PutObject") to
check several at once. The resource ARN is optional; without it the action
is simulated against "*".

The simulator's blind spots are printed with every verdict: resource-based
policies, session policies, and unsupplied condition keys are NOT evaluated,
so a real request can still differ.

Requires the iam:SimulatePrincipalPolicy (and iam:GetRole/GetUser for ARN
resolution) IAM permissions. IAM is global; no region needed.`,
	Example: `  # Why can't this role read the bucket?
  aws_explorer iam can arn:aws:iam::123456789012:role/app s3:GetObject arn:aws:s3:::my-bucket/key

  # Several actions at once, no specific resource
  aws_explorer iam can arn:aws:iam::123456789012:role/app ec2:StartInstances,ec2:StopInstances

  # Machine-readable
  aws_explorer iam can arn:aws:iam::123456789012:user/alice s3:PutObject -o json`,
	Args: cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		principalARN := strings.TrimSpace(args[0])
		if !strings.HasPrefix(principalARN, "arn:") {
			return fmt.Errorf("the principal must be a role or user ARN (got %q)", principalARN)
		}
		var actions []string
		for _, a := range strings.Split(args[1], ",") {
			if a = strings.TrimSpace(a); a != "" {
				actions = append(actions, a)
			}
		}
		if len(actions) == 0 {
			return fmt.Errorf("no action given (e.g. s3:GetObject)")
		}
		var resources []string
		if len(args) == 3 {
			resources = []string{strings.TrimSpace(args[2])}
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		region := "us-east-1"
		if awsRegion != "" {
			region = awsRegion
		} else if len(AppConfig.AWS.Regions) > 0 {
			region = AppConfig.AWS.Regions[0]
		}
		awscfg, err := auth.BuildAWSConfig(ctx, &AppConfig.AWS, region)
		if err != nil {
			if hint, ok := awserr.LoginHint(err, AppConfig.AWS.Profile); ok {
				return errors.New(hint)
			}
			return fmt.Errorf("unable to load AWS config: %w", err)
		}

		input := &iam.SimulatePrincipalPolicyInput{
			PolicySourceArn: aws.String(principalARN),
			ActionNames:     actions,
		}
		if len(resources) > 0 {
			input.ResourceArns = resources
		}

		client := iam.NewFromConfig(awscfg)
		var verdicts []iamsim.Verdict
		for {
			out, err := client.SimulatePrincipalPolicy(ctx, input)
			if err != nil {
				switch {
				case awserr.IsExpiredCreds(err):
					hint, _ := awserr.LoginHint(err, AppConfig.AWS.Profile)
					return errors.New(hint)
				case awserr.IsAuthError(err):
					return fmt.Errorf("not authorized to simulate — grant the iam:SimulatePrincipalPolicy IAM permission")
				case strings.Contains(err.Error(), "NoSuchEntity"):
					return fmt.Errorf("principal not found: %s (is the ARN exact, and in this account?)", principalARN)
				default:
					return fmt.Errorf("simulation failed: %w", err)
				}
			}
			verdicts = append(verdicts, iamsim.FromSDK(out.EvaluationResults)...)
			if !out.IsTruncated || out.Marker == nil {
				break
			}
			input.Marker = out.Marker
		}

		if strings.EqualFold(outputFormat, "json") || strings.EqualFold(outputFormat, "ndjson") {
			enc := json.NewEncoder(os.Stdout)
			if strings.EqualFold(outputFormat, "json") {
				enc.SetIndent("", "  ")
				return enc.Encode(verdicts)
			}
			for _, v := range verdicts {
				if err := enc.Encode(v); err != nil {
					return err
				}
			}
			return nil
		}

		iamsim.Render(os.Stdout, trail.ShortPrincipal(principalARN), verdicts)
		return nil
	},
}

func init() {
	iamCmd.AddCommand(iamCanCmd)
}
