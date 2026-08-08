package cmd

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/sqstui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	sqsQueue string
	sqsTheme string
)

var sqsCmd = &cobra.Command{
	Use:   "sqs",
	Short: "Start the SQS Queue Explorer TUI",
	Long: `Start an interactive TUI for exploring SQS queues: attributes, tags,
dead-letter relationships, CloudWatch metric sparklines, an opt-in message
peek, and a jump into the CloudWatch Logs explorer for a queue's Lambda
consumers.

Peeking at messages never deletes them, but SQS increments each sampled
message's receive count — the peek states this and asks for confirmation
before running, since on a queue with a redrive policy it moves messages
closer to the DLQ.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region; otherwise the config's aws.regions list
is used.`,
	Example: `  # Browse queues in one region
  aws_explorer sqs --region us-east-1

  # Pre-filter to queues whose name starts with "orders"
  aws_explorer sqs -q orders`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sqsCfg := tuiAWSConfig()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, sqsTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceScanLogs()

		var regions []string
		scanAll := false
		switch {
		case awsRegion != "":
			regions = []string{awsRegion}
		case allRegions || (AppConfig != nil && AppConfig.AWS.AllRegions):
			scanAll = true
		case AppConfig != nil && len(AppConfig.AWS.Regions) > 0:
			regions = AppConfig.AWS.Regions
		default:
			regions = []string{"us-east-1"}
		}

		m, err := sqstui.NewModel(ctx, sqsCfg, regions, scanAll, configFilePath(), AppConfig, sqsQueue)
		if err != nil {
			return fmt.Errorf("initializing SQS TUI: %w", err)
		}

		p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running SQS TUI: %w", err)
		}
		return nil
	},
}

func init() {
	sqsCmd.Flags().StringVarP(&sqsQueue, "queue", "q", "", "Initial queue name filter (also applied as a server-side name prefix)")
	sqsCmd.Flags().StringVar(&sqsTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(sqsCmd)
	registerThemeCompletion(sqsCmd, ui.ThemeNames())
	rootCmd.AddCommand(sqsCmd)
}
