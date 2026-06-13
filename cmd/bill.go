package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/auth"
	"github.com/ryandam9/aws_explorer/internal/awserr"
	"github.com/ryandam9/aws_explorer/internal/billing"
	"github.com/ryandam9/aws_explorer/internal/billtui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

// costExplorerRegion is the global Cost Explorer endpoint's home region; the
// API is account-wide, so the region only fixes which endpoint we talk to.
const costExplorerRegion = "us-east-1"

// minBillInterval guards the auto-refresh cadence: each refresh is a paid
// ($0.01) Cost Explorer request, and the numbers only move every few minutes,
// so a tight loop just burns money.
const minBillInterval = 1 * time.Minute

var (
	billMonth    string
	billTUI      bool
	billInterval time.Duration
)

var billCmd = &cobra.Command{
	Use:   "bill",
	Short: "Show the account's bill from Cost Explorer (live --tui)",
	Long: `Bill shows the account's cost for a billing period, grouped by service and
usage type, with the usage quantity for each line and a grand total — the
numbers the Billing console shows, pulled from the AWS Cost Explorer API.

By default it reports the current month to date (today's charges are
estimated and flagged as such); --month YYYY-MM reports a past month.

--tui opens a live screen that re-fetches on a fixed interval (--interval,
default 5m), so any activity that incurs cost surfaces without restarting.
A Δ column shows what moved since the previous refresh, and pressing x on a
line lists that service's per-resource costs (resource ID / ARN, usage,
amount) when the account has resource-level data enabled.

Cost note: Cost Explorer is a paid API — AWS bills every request at $0.01,
including each automatic refresh in --tui. The live screen names the cadence
so the cost is visible; raise --interval to spend less.

Required IAM: ce:GetCostAndUsage (and ce:GetCostAndUsageWithResources for the
per-resource drill-down). The call is read-only.`,
	Example: `  # Current month to date, grouped by service and usage type
  aws_explorer bill

  # A past month, machine-readable
  aws_explorer bill --month 2026-05 -o json

  # Live screen, refreshing every 10 minutes
  aws_explorer bill --tui --interval 10m

  # CSV for a spreadsheet
  aws_explorer bill -o csv --no-header > bill.csv`,
	RunE: func(cmd *cobra.Command, args []string) error {
		applyGlobalAWSOverrides()

		now := time.Now().UTC()
		var (
			start, end time.Time
			err        error
		)
		if billMonth != "" {
			if start, end, err = billing.ParseMonth(billMonth, now); err != nil {
				return err
			}
		} else {
			start, end = billing.MonthToDate(now)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Cost Explorer is global; pin the client to the endpoint's region
		// regardless of the configured scan regions.
		awscfg, err := auth.BuildAWSConfig(ctx, &AppConfig.AWS, costExplorerRegion)
		if err != nil {
			if hint, ok := awserr.LoginHint(err, AppConfig.AWS.Profile); ok {
				return fmt.Errorf("%s", hint)
			}
			return fmt.Errorf("unable to load AWS config: %w", err)
		}
		api := billing.NewClient(awscfg)

		if billTUI {
			if billInterval < minBillInterval {
				return fmt.Errorf("--interval must be at least %s (each refresh is a paid Cost Explorer request)", minBillInterval)
			}
			ui.InitFromConfig(AppConfig.UI)
			SilenceScanLogs()
			label := billing.PeriodLabel(start, end, now)
			m := billtui.New(ctx, api, start, end, label, billInterval, AppConfig.AWS.Profile)
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running bill TUI: %v\n", err)
				os.Exit(1)
			}
			return nil
		}

		fmt.Fprintf(os.Stderr, "Fetching the bill for %s…\n", billing.PeriodLabel(start, end, now))
		bill, err := billing.Fetch(ctx, api, start, end)
		if err != nil {
			if hint, ok := awserr.LoginHint(err, AppConfig.AWS.Profile); ok {
				return fmt.Errorf("%s", hint)
			}
			return fmt.Errorf("fetching the bill: %w", err)
		}

		if len(bill.Lines) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Printf("Nothing billed for %s.\n", billing.PeriodLabel(start, end, now))
			return nil
		}
		if err := billing.Render(os.Stdout, bill, outputFormat, noHeader); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering the bill: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	billCmd.Flags().StringVar(&billMonth, "month", "",
		"billing period as YYYY-MM (default: current month to date)")
	billCmd.Flags().BoolVar(&billTUI, "tui", false,
		"open the live bill screen instead of printing once")
	billCmd.Flags().DurationVar(&billInterval, "interval", 5*time.Minute,
		"auto-refresh cadence for --tui (each refresh is a paid Cost Explorer request)")
	rootCmd.AddCommand(billCmd)
}
