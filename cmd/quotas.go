package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/quotas"
)

var quotasThreshold float64

var quotasCmd = &cobra.Command{
	Use:   "quotas",
	Short: "Service-quota dashboard — limits closest to exhaustion",
	Long: `Quotas reports a curated set of the AWS limits that actually cause incidents
— vCPUs, Elastic IPs, VPCs, network interfaces, Lambda concurrency, RDS
instances, EBS storage, load balancers, EKS clusters, IAM roles — with their
real limits and current usage, sorted so the ones nearest the ceiling lead.

Limits come from the Service Quotas API's applied value, so account-specific
increases are reflected (the VPC linter uses hardcoded defaults; this does
not). When a quota has never been adjusted, the AWS default is shown instead.

Usage comes from each quota's CloudWatch usage metric where AWS publishes one;
quotas without a usage metric are listed with their limit but no percentage
(shown only with --threshold 0) rather than a guess.

The report is read-only and best-effort: a denied or failed lookup skips that
quota (reported on stderr) and never aborts the run.`,
	Example: `  # Quotas at or above 80% utilization (default), current region
  aws_explorer quotas

  # Tighter alerting threshold, across all regions
  aws_explorer quotas --threshold 90 --all-regions

  # Everything, including quotas with no usage metric
  aws_explorer quotas --threshold 0

  # Machine-readable; page on anything critical
  aws_explorer quotas --threshold 0 -o json | jq '[.[] | select(.status=="critical")]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if quotasThreshold < 0 || quotasThreshold > 100 {
			return fmt.Errorf("--threshold must be between 0 and 100")
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		fmt.Fprintf(os.Stderr, "Checking service quotas in %d region(s)…\n", len(regions))

		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second
		collected, errs := quotas.Collect(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout)
		output.PrintErrors(os.Stderr, errs)

		// Evaluate against the threshold (used both as the warn level and the
		// display filter), then narrow to the rows worth attention.
		rows := quotas.Evaluate(collected, quotasThreshold)
		rows, hidden := quotas.Filter(rows, quotasThreshold)

		if len(rows) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Printf("No quotas at or above %.0f%% utilization.", quotasThreshold)
			if hidden > 0 {
				fmt.Printf(" (%d quota(s) below the threshold or without a usage metric — use --threshold 0 to list all.)", hidden)
			}
			fmt.Println()
			return nil
		}
		if err := quotas.Render(os.Stdout, rows, outputFormat, noHeader); err != nil {
			return fmt.Errorf("rendering report: %w", err)
		}
		if hidden > 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Fprintf(os.Stderr, "\n%d quota(s) below %.0f%% or without a usage metric hidden — use --threshold 0 to list all.\n", hidden, quotasThreshold)
		}
		return nil
	},
}

func init() {
	quotasCmd.Flags().Float64Var(&quotasThreshold, "threshold", 80,
		"only show quotas at or above this % utilization (0 = show all, including those with no usage metric)")
	rootCmd.AddCommand(quotasCmd)
}
