package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/discovery"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/summary"
	"github.com/ryandam9/aws_explorer/internal/tui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	summaryTUI       bool
	summaryTypedOnly bool
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "List every AWS resource across all regions",
	Long: `Summary lists all discoverable AWS resources across every configured
region as a single numbered inventory. Each row shows the serial number, the
resource name (or "-" when it has none), the resource type, the ARN, and the
region (with availability zone when the resource is zonal).

By default the inventory is printed as a table (use -o json|ndjson|csv for
other formats). Pass --tui to explore the same data interactively.`,
	Example: `  # Full inventory of every region
  aws_explorer summary --all-regions

  # One region, exported as CSV
  aws_explorer summary -r us-east-1 -o csv > inventory.csv

  # Explore interactively
  aws_explorer summary --tui`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		applyGlobalAWSOverrides()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		if summaryTUI {
			ui.InitFromConfig(AppConfig.UI)
			// The TUI owns the screen; keep scan logs from corrupting it.
			SilenceScanLogs()
			// Gather the all-services sweep up front and seed the TUI with it;
			// the typed collectors then stream in and merge (deduped by ARN).
			var seed []model.Resource
			if !summaryTypedOnly {
				fmt.Fprintln(os.Stderr, "Discovering resources across all services…")
				seed, _ = discovery.Discover(ctx, eng.AWSConfig, eng.EffectiveRegions(), AppConfig.App.MaxConcurrency)
			}
			m := tui.NewModelWithSeed(ctx, eng, configFilePath(), AppConfig, seed)
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running summary TUI: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Problems are summarized after the run; the raw log stream would
		// only interleave with the table.
		SilenceScanLogs()

		result, err := eng.Run(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error collecting resources: %v\n", err)
			os.Exit(1)
		}

		resources := result.Resources
		errs := result.Errors

		// Universal sweep across all services via the Resource Groups Tagging
		// API, merged with the rich typed collectors above (deduped by ARN).
		if !summaryTypedOnly {
			discovered, dErrs := discovery.Discover(
				ctx, eng.AWSConfig, eng.EffectiveRegions(), AppConfig.App.MaxConcurrency)
			resources = append(resources, discovered...)
			errs = append(errs, dErrs...)
		}

		output.PrintErrors(os.Stderr, errs)

		rows := summary.BuildRows(resources)
		if len(rows) == 0 {
			fmt.Println("No resources found.")
			return
		}
		if err := summary.Render(os.Stdout, rows, outputFormat, noHeader); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering summary: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	summaryCmd.Flags().BoolVar(&summaryTUI, "tui", false, "Explore the inventory interactively instead of printing a table")
	summaryCmd.Flags().BoolVar(&summaryTypedOnly, "typed-only", false, "Only use the built-in typed collectors; skip the all-services Tagging API sweep")
	rootCmd.AddCommand(summaryCmd)
}
