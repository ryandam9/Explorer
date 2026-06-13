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
var auditCategories = []string{"cost", "security", "iam"}

// auditExitFindings is the exit code when --fail-on is set and findings at or
// above the threshold exist (operational errors exit 1, clean runs 0), so CI
// can distinguish "found waste" from "audit broke".
const auditExitFindings = 2

var (
	auditOnly   []string
	auditTUI    bool
	auditFailOn string
	auditIgnore []string
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Scan the account for cost waste and security risks (findings linter)",
	Long: `Audit scans the configured regions and reports findings in two categories
(both run by default; --only narrows):

cost — unattached EBS volumes, gp2 volumes that could be gp3, unassociated
Elastic IPs, idle NAT gateways, load balancers with no healthy targets or no
traffic, stopped instances still paying for EBS storage, old unreferenced
snapshots and AMIs, and over-provisioned DynamoDB tables. Each cost finding
carries an approximate monthly cost, with a total at the bottom (us-east-1
on-demand list prices; order-of-magnitude, not a bill).

security — public S3 buckets and missing Public Access Blocks, buckets
without default encryption, unencrypted EBS volumes and regions without
EBS encryption-by-default, publicly shared EBS/RDS snapshots, publicly
accessible or unencrypted RDS instances, EC2 instances still allowing
IMDSv1, security groups opening sensitive ports (SSH, RDP, databases) to
the internet, Lambda function URLs with no auth, SQS/SNS policies that
allow everyone, and alarms stuck in INSUFFICIENT_DATA.

iam — account-global hygiene via the credential report and policy scan:
root access keys, console users without MFA, access keys older than 90
days or active-but-unused, roles unused for 90+ days, customer policies
granting */*, trust policies allowing any AWS principal, and policies
attached directly to users.

Every finding carries a stable check ID (e.g. COST-EBS-001, SEC-S3-001,
IAM-KEY-001).

For CI pipelines, --fail-on <severity> exits 2 when findings at or above the
threshold exist (0 below it, 1 on operational errors), --ignore suppresses
individual checks by ID, and -o sarif emits SARIF 2.1.0 for upload to GitHub
code scanning.

The audit is read-only and best-effort: a denied API call skips the affected
checks (reported on stderr) and never aborts the run. Traffic-based checks
(idle load balancers, DynamoDB utilization) additionally need
cloudwatch:GetMetricData and are skipped without it.`,
	Example: `  # Audit the configured regions
  aws_explorer audit

  # Audit every region, machine-readable
  aws_explorer audit --all-regions -o json

  # Explore interactively
  aws_explorer audit --all-regions --tui

  # Security category only
  aws_explorer audit --only security

  # IAM hygiene only (account-global; region flags don't matter)
  aws_explorer audit --only iam

  # CI gate: exit 2 if any warning-or-worse finding exists
  aws_explorer audit --fail-on warning --ignore COST-EBS-002,SEC-EC2-001

  # SARIF for GitHub code scanning
  aws_explorer audit -o sarif > audit.sarif`,
	// audit accepts one format beyond the global set (sarif), so it replaces
	// the root command's format validation with its own.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if strings.EqualFold(outputFormat, "sarif") {
			return nil
		}
		return output.ValidateFormat(outputFormat)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateAuditCategories(auditOnly); err != nil {
			return err
		}
		ignore, err := parseIgnoreIDs(auditIgnore)
		if err != nil {
			return err
		}
		var failOn findings.Severity
		if auditFailOn != "" {
			if auditTUI {
				return fmt.Errorf("--fail-on is for scripting and cannot be combined with --tui")
			}
			if failOn, err = findings.ParseSeverity(auditFailOn); err != nil {
				return err
			}
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

		categories := auditOnly
		if len(categories) == 0 {
			categories = auditCategories
		}

		if auditTUI {
			ui.InitFromConfig(AppConfig.UI)
			ch := make(chan audit.CostChunk, 8)
			go audit.Stream(ctx, eng.AWSConfig, regions, categories, AppConfig.App.MaxConcurrency, timeout, ch)
			m := audittui.New(regions, dropIgnored(ch, ignore))
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running audit TUI: %v\n", err)
				os.Exit(1)
			}
			return nil
		}

		fmt.Fprintf(os.Stderr, "Auditing %d region(s) for %s…\n", len(regions), strings.Join(categories, " + "))
		fs, errs := audit.Run(ctx, eng.AWSConfig, regions, categories, AppConfig.App.MaxConcurrency, timeout)
		fs = findings.Drop(fs, ignore)

		output.PrintErrors(os.Stderr, errs)

		if err := renderAuditFindings(fs); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering findings: %v\n", err)
			os.Exit(1)
		}
		if auditFailOn != "" && findings.AnyAtOrAbove(fs, failOn) {
			os.Exit(auditExitFindings)
		}
		return nil
	},
}

// renderAuditFindings writes the findings in the requested output format.
func renderAuditFindings(fs []findings.Finding) error {
	if strings.EqualFold(outputFormat, "sarif") {
		return findings.RenderSARIF(os.Stdout, fs, version)
	}
	if len(fs) == 0 && strings.EqualFold(outputFormat, "table") {
		fmt.Println("No findings.")
		return nil
	}
	return findings.Render(os.Stdout, fs, outputFormat, noHeader)
}

// dropIgnored wraps the chunk stream, removing suppressed findings before the
// TUI sees them.
func dropIgnored(ch <-chan audit.CostChunk, ignore map[string]bool) <-chan audit.CostChunk {
	if len(ignore) == 0 {
		return ch
	}
	out := make(chan audit.CostChunk, 8)
	go func() {
		defer close(out)
		for c := range ch {
			c.Findings = findings.Drop(c.Findings, ignore)
			out <- c
		}
	}()
	return out
}

// parseIgnoreIDs validates --ignore values against the check registry, so a
// typo fails loudly instead of silently suppressing nothing.
func parseIgnoreIDs(ids []string) (map[string]bool, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ignore := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.ToUpper(strings.TrimSpace(id))
		if _, ok := findings.CheckByID(id); !ok {
			return nil, fmt.Errorf("unknown check ID %q in --ignore (known IDs: %s)", id, strings.Join(knownCheckIDs(), ", "))
		}
		ignore[id] = true
	}
	return ignore, nil
}

func knownCheckIDs() []string {
	checks := findings.Checks()
	ids := make([]string, len(checks))
	for i, c := range checks {
		ids[i] = c.ID
	}
	return ids
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
	auditCmd.Flags().StringVar(&auditFailOn, "fail-on", "",
		"exit with code 2 if findings at or above this severity exist: critical, warning, info")
	auditCmd.Flags().StringSliceVar(&auditIgnore, "ignore", nil,
		"suppress findings by check ID (e.g. COST-EBS-002)")
	_ = auditCmd.RegisterFlagCompletionFunc("only",
		cobra.FixedCompletions(auditCategories, cobra.ShellCompDirectiveNoFileComp))
	_ = auditCmd.RegisterFlagCompletionFunc("fail-on",
		cobra.FixedCompletions([]string{"critical", "warning", "info"}, cobra.ShellCompDirectiveNoFileComp))
	_ = auditCmd.RegisterFlagCompletionFunc("ignore",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return knownCheckIDs(), cobra.ShellCompDirectiveNoFileComp
		})
	rootCmd.AddCommand(auditCmd)
}
