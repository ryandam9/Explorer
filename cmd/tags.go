package cmd

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/tagstui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var tagsTheme string

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "Explore AWS resources by tag",
	Long: `Explore AWS resources by tag. Browse the account's tag keys, drill into a
key's values, and press Enter to list every resource carrying that tag — or
press f to type one or more Key=Value filters directly (comma-separated; repeat
a key to OR its values; a bare key matches any value).

Data comes from the Resource Groups Tagging API, so only tagged resources on
services that integrate with it are shown (IAM, for example, is not). On a
resource, y copies its ARN and o opens it in the AWS console.

Scope: --region pins a single region; --all-regions (or aws.allRegions in the
config) sweeps every enabled region; otherwise the config's aws.regions list is
used (defaulting to us-east-1).`,
	Example: `  # Browse tags in the configured region
  aws_explorer tags

  # Sweep every region
  aws_explorer tags --all-regions`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ui.InitFromConfig(AppConfig.UI)
		activeTheme := resolveTheme(cmd, tagsTheme)
		if idx, ok := ui.LookupTheme(activeTheme); ok {
			ui.SetActiveTheme(idx)
		}
		SilenceScanLogs()

		regions, scanAll := tagsRegionScope()
		client, err := tagstui.NewClient(ctx, tuiAWSConfig(), regions, scanAll)
		if err != nil {
			return fmt.Errorf("initializing tags dashboard: %w", err)
		}

		model := tagstui.NewModel(ctx, client, scanAll)
		p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running tags dashboard: %w", err)
		}
		return nil
	},
}

// tagsRegionScope resolves the region list and all-regions flag the same way the
// other dashboards do.
func tagsRegionScope() ([]string, bool) {
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

func init() {
	tagsCmd.Flags().StringVar(&tagsTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerAlwaysTUIFlag(tagsCmd)
	registerThemeCompletion(tagsCmd, ui.ThemeNames())
	rootCmd.AddCommand(tagsCmd)
}
