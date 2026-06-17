package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/lambdatui"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var lambdaTheme string

var lambdaCmd = &cobra.Command{
	Use:   "lambda",
	Short: "Start the AWS Lambda dashboard TUI",
	Long: `Start an interactive dashboard for AWS Lambda: functions (with their runtime,
memory, timeout and state), layers (latest version and compatible runtimes) and
event-source mappings (source, state and batch size). Press Enter on a function
to drill into its full configuration — role, layers, VPC, dead-letter queue,
reserved concurrency, environment-variable keys (values never shown), code
location and tags, fetched on demand. Press f for the findings panel:
deterministic runtime/health checks (deprecated runtimes, missing dead-letter
queues, failed-state functions). On a function, L opens its CloudWatch logs.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column; otherwise the
config's aws.regions list is used.`,
	Example: `  # Browse Lambda in the configured regions
  aws_explorer lambda

  # Pin one region
  aws_explorer lambda --region us-east-1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		lambdaCfg := tuiAWSConfig()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, lambdaTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := lambdaRegionScope()

		model, err := lambdatui.NewModel(ctx, lambdaCfg, regions, scanAll, AppConfig, configFilePath())
		if err != nil {
			return fmt.Errorf("initializing Lambda dashboard: %w", err)
		}

		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running Lambda dashboard: %w", err)
		}
		return nil
	},
}

// lambdaRegionScope resolves the region list and all-regions flag the same way
// the dashboard does, so the CLI twins honour --region / --all-regions / config.
func lambdaRegionScope() ([]string, bool) {
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

// newLambdaClient builds the shared Lambda client for the CLI twins.
func newLambdaClient(ctx context.Context) (*lambdatui.Client, error) {
	regions, scanAll := lambdaRegionScope()
	return lambdatui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
}

var lambdaFunctionsCmd = &cobra.Command{
	Use:     "functions",
	Short:   "List Lambda functions with their runtime, memory, timeout and state",
	Example: "  aws_explorer lambda functions --all-regions -o json",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderFunctions(os.Stdout, inv.Functions, outputFormat, noHeader)
	},
}

var lambdaLayersCmd = &cobra.Command{
	Use:   "layers",
	Short: "List Lambda layers with their latest version and compatible runtimes",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderLayers(os.Stdout, inv.Layers, outputFormat, noHeader)
	},
}

var lambdaEventSourcesCmd = &cobra.Command{
	Use:     "event-sources",
	Short:   "List Lambda event-source mappings (source, state, batch size)",
	Aliases: []string{"event-source-mappings", "esm"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := output.ValidateFormat(outputFormat); err != nil {
			return err
		}
		ctx := context.Background()
		SilenceScanLogs()
		client, err := newLambdaClient(ctx)
		if err != nil {
			return err
		}
		inv, err := client.LoadInventory(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		return lambdatui.RenderEventSources(os.Stdout, inv.EventSources, outputFormat, noHeader)
	},
}

func init() {
	lambdaCmd.Flags().StringVar(&lambdaTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(lambdaCmd)
	registerThemeCompletion(lambdaCmd, ui.ThemeNames())

	lambdaCmd.AddCommand(lambdaFunctionsCmd, lambdaLayersCmd, lambdaEventSourcesCmd)
	rootCmd.AddCommand(lambdaCmd)
}
