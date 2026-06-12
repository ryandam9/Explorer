package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/vpctui"
)

var vpcTheme string

var vpcCmd = &cobra.Command{
	Use:   "vpc",
	Short: "Start the VPC Explorer TUI",
	Long: `Start an interactive TUI for exploring VPCs and their associated resources
across regions: subnets, security groups, route tables, gateways, endpoints,
NACLs, peering, flow logs and attached compute, plus the VPC debugging
toolkit (findings linter, path tracer, exposure audit, snapshot diff).`,
	Example: `  # Explore VPCs in one region
  aws_explorer vpc --region us-east-1

  # Sweep every region with a named profile
  aws_explorer vpc --all-regions --profile prod`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		vpcCfg := tuiAWSConfig()
		activeTheme := resolveTheme(cmd, vpcTheme)

		ui.InitFromConfig(AppConfig.UI)
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceScanLogs()

		scanAll := allRegions
		if AppConfig != nil && AppConfig.AWS.AllRegions {
			scanAll = true
		}

		m, err := vpctui.NewModel(ctx, vpcCfg, awsRegion, scanAll, activeTheme, configFilePath(), AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing VPC TUI: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running VPC TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	vpcCmd.Flags().StringVar(&vpcTheme, "theme", defaultThemeName, "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	registerThemeCompletion(vpcCmd, ui.ThemeNames())
	rootCmd.AddCommand(vpcCmd)
}
