package cmd

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/cwtui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	cwGroup  string
	cwStream string
	cwFilter string
	cwTheme  string
)

var cwCmd = &cobra.Command{
	Use:   "cw",
	Short: "Start the CloudWatch Logs Explorer TUI",
	Long: `Start an interactive TUI for exploring, filtering, searching and tailing
CloudWatch log groups, streams and events.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region and adds a Region column to the group
list; otherwise the config's aws.regions list is used.`,
	Example: `  # Browse log groups in one region
  aws_explorer cw --region us-east-1

  # Open a group and tail events matching a pattern
  aws_explorer cw -g /aws/lambda/my-fn -f ERROR`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		cwCfg := tuiAWSConfig()

		// Initialize UI theme & colors.
		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, cwTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceScanLogs()

		// Region scope: --region pins a single region and wins over
		// everything; otherwise --all-regions / aws.allRegions sweeps every
		// enabled region; otherwise the config's aws.regions list; otherwise
		// us-east-1.
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

		m, err := cwtui.NewModel(ctx, cwCfg, regions, scanAll, configFilePath(), AppConfig, cwGroup, cwStream, cwFilter)
		if err != nil {
			return fmt.Errorf("initializing CloudWatch Logs TUI: %w", err)
		}

		p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))

		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running CloudWatch Logs TUI: %w", err)
		}
		return nil
	},
}

func init() {
	cwCmd.Flags().StringVarP(&cwGroup, "group", "g", "", "Initial CloudWatch log group filter/pattern")
	cwCmd.Flags().StringVarP(&cwStream, "stream", "s", "", "Initial CloudWatch log stream filter")
	cwCmd.Flags().StringVarP(&cwFilter, "filter", "f", "", "Initial query pattern for log events")
	cwCmd.Flags().StringVar(&cwTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(cwCmd)
	registerThemeCompletion(cwCmd, ui.ThemeNames())
	rootCmd.AddCommand(cwCmd)
}
