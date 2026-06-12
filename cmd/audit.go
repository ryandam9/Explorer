package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/audit"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/output"
)

// auditCategories lists the implemented finding categories. More join as the
// roadmap lands (security, messaging, …); --only validates against this.
var auditCategories = []string{"cost"}

var auditOnly []string

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Scan the account for cost waste (findings linter)",
	Long: `Audit scans the configured regions and reports findings — currently the
cost/waste category: unattached EBS volumes, gp2 volumes that could be gp3,
unassociated Elastic IPs, idle NAT gateways, load balancers with no healthy
targets or no traffic, stopped instances still paying for EBS storage, old
unreferenced snapshots and AMIs, and over-provisioned DynamoDB tables.

Each finding carries a stable check ID (e.g. COST-EBS-001) and an approximate
monthly cost, with a total at the bottom. Estimates use us-east-1 on-demand
list prices and are order-of-magnitude, not a bill.

The audit is read-only and best-effort: a denied API call skips the affected
checks (reported on stderr) and never aborts the run. Traffic-based checks
(idle load balancers, DynamoDB utilization) additionally need
cloudwatch:GetMetricData and are skipped without it.`,
	Example: `  # Audit the configured regions
  aws_explorer audit

  # Audit every region, machine-readable
  aws_explorer audit --all-regions -o json

  # Only the cost/waste checks (the default until more categories exist)
  aws_explorer audit --only cost`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateAuditCategories(auditOnly); err != nil {
			return err
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		// Problems are summarized after the run; the raw log stream would
		// only interleave with the table.
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		fmt.Fprintf(os.Stderr, "Auditing %d region(s) for cost waste…\n", len(regions))

		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second
		fs, errs := audit.Cost(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout)

		output.PrintErrors(os.Stderr, errs)

		if len(fs) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Println("No findings.")
			return nil
		}
		if err := findings.Render(os.Stdout, fs, outputFormat, noHeader); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering findings: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}

// validateAuditCategories rejects unknown --only values up front.
func validateAuditCategories(only []string) error {
	for _, c := range only {
		known := false
		for _, k := range auditCategories {
			if strings.EqualFold(c, k) {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown audit category %q (available: %s)", c, strings.Join(auditCategories, ", "))
		}
	}
	return nil
}

func init() {
	auditCmd.Flags().StringSliceVar(&auditOnly, "only", nil,
		"restrict to these finding categories (available: "+strings.Join(auditCategories, ", ")+")")
	_ = auditCmd.RegisterFlagCompletionFunc("only",
		cobra.FixedCompletions(auditCategories, cobra.ShellCompDirectiveNoFileComp))
	rootCmd.AddCommand(auditCmd)
}
