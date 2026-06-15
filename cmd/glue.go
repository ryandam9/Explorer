package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/gluetui"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var glueTheme string

var glueCmd = &cobra.Command{
	Use:   "glue",
	Short: "Start the AWS Glue dashboard TUI",
	Long: `Start an interactive dashboard for AWS Glue: jobs (with their latest run
state and duration), crawlers, triggers, workflows, connections and the Data
Catalog. Press Enter on a job to drill into its run history — state, duration,
DPU-hours and an estimated cost per run, with the error message inline on
failures.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column; otherwise the
config's aws.regions list is used.`,
	Example: `  # Browse Glue in the configured regions
  aws_explorer glue

  # Pin one region
  aws_explorer glue --region us-east-1`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		glueCfg := tuiAWSConfig()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, glueTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := glueRegionScope()

		model, err := gluetui.NewModel(ctx, glueCfg, regions, scanAll, AppConfig, configFilePath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing Glue dashboard: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running Glue dashboard: %v\n", err)
			os.Exit(1)
		}
	},
}

var (
	glueRunsLimit  int32
	glueRunsStatus string
)

// glueRegionScope resolves the region list and all-regions flag the same way
// the dashboard does, so the CLI twins honour --region / --all-regions / config.
func glueRegionScope() ([]string, bool) {
	switch {
	case awsRegion != "":
		return []string{awsRegion}, false
	case allRegions || (AppConfig != nil && AppConfig.AWS.AllRegions):
		return nil, true
	case AppConfig != nil && len(AppConfig.AWS.Regions) > 0:
		return AppConfig.AWS.Regions, false
	default:
		return []string{"us-east-1"}, false
	}
}

// newGlueClient builds the shared Glue client for the CLI twins.
func newGlueClient(ctx context.Context) (*gluetui.Client, error) {
	regions, scanAll := glueRegionScope()
	return gluetui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
}

var glueJobsCmd = &cobra.Command{
	Use:     "jobs",
	Short:   "List Glue jobs with their latest run state",
	Example: "  aws_explorer glue jobs --all-regions -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newGlueClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return gluetui.RenderJobs(os.Stdout, inv.Jobs, outputFormat, noHeader)
	},
}

var glueCrawlersCmd = &cobra.Command{
	Use:   "crawlers",
	Short: "List Glue crawlers with their last-crawl status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newGlueClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return gluetui.RenderCrawlers(os.Stdout, inv.Crawlers, outputFormat, noHeader)
	},
}

var glueTriggersCmd = &cobra.Command{
	Use:   "triggers",
	Short: "List Glue triggers",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newGlueClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return gluetui.RenderTriggers(os.Stdout, inv.Triggers, outputFormat, noHeader)
	},
}

var glueWorkflowsCmd = &cobra.Command{
	Use:   "workflows",
	Short: "List Glue workflows",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newGlueClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return gluetui.RenderWorkflows(os.Stdout, inv.Workflows, outputFormat, noHeader)
	},
}

var glueRunsCmd = &cobra.Command{
	Use:   "runs <job-name>",
	Short: "Show a Glue job's run history (state, duration, DPU-hours, cost)",
	Args:  cobra.ExactArgs(1),
	Example: `  aws_explorer glue runs nightly-orders-etl -r us-east-1
  aws_explorer glue runs nightly-orders-etl --status FAILED -o json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newGlueClient(ctx)
		if err != nil {
			return err
		}
		// Runs are region-specific: use --region when given, else the first
		// region in scope.
		region := awsRegion
		if region == "" && len(client.Regions()) > 0 {
			region = client.Regions()[0]
		}
		runs, err := client.JobRuns(ctx, region, args[0], glueRunsLimit)
		if err != nil {
			return fmt.Errorf("failed to get runs for job %q in %s: %w", args[0], region, err)
		}
		runs = gluetui.FilterRunsByStatus(runs, glueRunsStatus)
		return gluetui.RenderRuns(os.Stdout, runs, outputFormat, noHeader)
	},
}

func init() {
	glueCmd.Flags().StringVar(&glueTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(glueCmd)
	registerThemeCompletion(glueCmd, ui.ThemeNames())

	glueRunsCmd.Flags().Int32Var(&glueRunsLimit, "limit", 20, "maximum number of runs to fetch")
	glueRunsCmd.Flags().StringVar(&glueRunsStatus, "status", "", "only show runs in this state (e.g. FAILED, SUCCEEDED)")

	glueCmd.AddCommand(glueJobsCmd, glueCrawlersCmd, glueTriggersCmd, glueWorkflowsCmd, glueRunsCmd)
	rootCmd.AddCommand(glueCmd)
}
