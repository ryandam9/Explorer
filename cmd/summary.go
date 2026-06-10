package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/output"
	"github.com/user/aws_explorer/internal/summary"
	"github.com/user/aws_explorer/internal/tui"
	"github.com/user/aws_explorer/internal/ui"
)

var summaryTUI bool

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "List every AWS resource across all regions",
	Long: `Summary lists all discoverable AWS resources across every configured
region as a single numbered inventory. Each row shows the serial number, the
resource name (or "-" when it has none), the resource type, the ARN, and the
region (with availability zone when the resource is zonal).

By default the inventory is printed as a table (use -o json|csv for other
formats). Pass --tui to explore the same data interactively.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		applyGlobalAWSOverrides()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		if summaryTUI {
			ui.InitFromConfig(AppConfig.UI)
			m := tui.NewModel(ctx, eng, configFilePath(), AppConfig)
			p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "Error running summary TUI: %v\n", err)
				os.Exit(1)
			}
			return
		}

		result, err := eng.Run(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error collecting resources: %v\n", err)
			os.Exit(1)
		}

		if len(result.Errors) > 0 {
			output.PrintErrors(os.Stderr, result.Errors)
		}

		rows := summary.BuildRows(result.Resources)
		if len(rows) == 0 {
			fmt.Println("No resources found.")
			return
		}
		if err := summary.Render(os.Stdout, rows, outputFormat); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering summary: %v\n", err)
			os.Exit(1)
		}
	},
}

// applyGlobalAWSOverrides applies the persistent CLI auth flags onto AppConfig,
// mirroring the behaviour of the root and tui commands.
func applyGlobalAWSOverrides() {
	if allRegions {
		AppConfig.AWS.AllRegions = true
	}
	if awsProfile != "" {
		AppConfig.AWS.Profile = awsProfile
	}
	if awsAuthMethod != "" {
		AppConfig.AWS.AuthMethod = awsAuthMethod
	}
	if awsRoleARN != "" {
		AppConfig.AWS.STS.RoleARN = awsRoleARN
		if AppConfig.AWS.AuthMethod == "" || AppConfig.AWS.AuthMethod == "auto" {
			AppConfig.AWS.AuthMethod = "sts"
		}
	}
}

func init() {
	summaryCmd.Flags().BoolVar(&summaryTUI, "tui", false, "Explore the inventory interactively instead of printing a table")
	rootCmd.AddCommand(summaryCmd)
}
