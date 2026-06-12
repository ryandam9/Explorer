package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/audit"
	"github.com/ryandam9/aws_explorer/internal/audittui"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/findings"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// auditCategories lists the implemented finding categories. More join as the
// roadmap lands (security, messaging, …); --only validates against this.
var auditCategories = []string{"cost"}

var (
	auditOnly []string
	auditTUI  bool
)

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
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		// Problems are summarized after the run (CLI) or shown in the errors
		// overlay (TUI); the raw log stream would only corrupt the screen.
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second

		if auditTUI {
			ui.InitFromConfig(AppConfig.UI)
			ch := make(chan audit.CostChunk, 8)
			go audit.StreamCost(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, ch)
			m := audittui.New(regions, ch)
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running audit TUI: %v\n", err)
				os.Exit(1)
			}
			return nil
		}

		fmt.Fprintf(os.Stderr, "Auditing %d region(s) for cost waste…\n", len(regions))
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
	auditCmd.Flags().BoolVar(&auditTUI, "tui", false,
		"explore the findings interactively instead of printing")
	_ = auditCmd.RegisterFlagCompletionFunc("only",
		cobra.FixedCompletions(auditCategories, cobra.ShellCompDirectiveNoFileComp))
	rootCmd.AddCommand(auditCmd)
}
