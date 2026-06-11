package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/ryandam9/aws_explorer/internal/config"
	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/vpctui"
)

var (
	vpcProfile    string
	vpcAuthMethod string
	vpcRoleARN    string
	vpcRegion     string
	vpcTheme      string
	vpcAllRegions bool
)

var vpcCmd = &cobra.Command{
	Use:   "vpc",
	Short: "Start the VPC Explorer TUI",
	Long:  `Start an interactive TUI for exploring VPCs and their associated resources across regions.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		vpcCfg := &config.AWSConfig{
			Profile:    awsProfile,
			AuthMethod: awsAuthMethod,
		}
		if vpcProfile != "" {
			vpcCfg.Profile = vpcProfile
		}
		if vpcAuthMethod != "" {
			vpcCfg.AuthMethod = vpcAuthMethod
		}
		roleARN := awsRoleARN
		if vpcRoleARN != "" {
			roleARN = vpcRoleARN
		}
		if roleARN != "" {
			vpcCfg.STS.RoleARN = roleARN
			if vpcCfg.AuthMethod == "" || vpcCfg.AuthMethod == "auto" {
				vpcCfg.AuthMethod = "sts"
			}
		}

		activeTheme := vpcTheme
		if AppConfig != nil && AppConfig.UI.Theme != "" && vpcTheme == "spotted-pardalote" {
			activeTheme = AppConfig.UI.Theme
		}
		ui.InitFromConfig(AppConfig.UI)
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceLogsForTUI()

		scanAll := vpcAllRegions
		if AppConfig != nil && AppConfig.AWS.AllRegions {
			scanAll = true
		}

		m, err := vpctui.NewModel(ctx, vpcCfg, vpcRegion, scanAll, activeTheme, configFilePath(), AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing VPC TUI: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running VPC TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	vpcCmd.Flags().StringVar(&vpcProfile, "profile", "", "AWS named profile (overrides global --profile)")
	vpcCmd.Flags().StringVar(&vpcAuthMethod, "auth-method", "", "Auth method: auto, profile, env, static, sts")
	vpcCmd.Flags().StringVar(&vpcRoleARN, "role-arn", "", "IAM role ARN to assume via STS")
	vpcCmd.Flags().StringVar(&vpcRegion, "region", "", "AWS region (defaults to all regions if omitted)")
	vpcCmd.Flags().StringVar(&vpcTheme, "theme", "spotted-pardalote", "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	vpcCmd.Flags().BoolVar(&vpcAllRegions, "all-regions", false, "Scan all AWS regions")
	rootCmd.AddCommand(vpcCmd)
}
