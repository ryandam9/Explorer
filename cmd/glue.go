package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/gluetui"
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

		model, err := gluetui.NewModel(ctx, glueCfg, regions, scanAll, AppConfig)
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

func init() {
	glueCmd.Flags().StringVar(&glueTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(glueCmd)
	registerThemeCompletion(glueCmd, ui.ThemeNames())
	rootCmd.AddCommand(glueCmd)
}
