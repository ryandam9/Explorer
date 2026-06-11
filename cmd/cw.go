package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/user/aws_explorer/internal/config"
	"github.com/user/aws_explorer/internal/cwtui"
	"github.com/user/aws_explorer/internal/ui"
)

var (
	cwGroup      string
	cwStream     string
	cwFilter     string
	cwProfile    string
	cwAuthMethod string
	cwRoleARN    string
	cwRegion     string
	cwTheme      string
	cwTui        bool
)

var cwCmd = &cobra.Command{
	Use:   "cw",
	Short: "Start the CloudWatch Logs Explorer TUI",
	Long:  `Start a highly interactive terminal user interface (TUI) for exploring, filtering, searching and tailing CloudWatch log groups, streams and events.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Build auth config, preferring local cw flags over global ones.
		cwCfg := &config.AWSConfig{
			Profile:    awsProfile,
			AuthMethod: awsAuthMethod,
		}
		if cwProfile != "" {
			cwCfg.Profile = cwProfile
		}
		if cwAuthMethod != "" {
			cwCfg.AuthMethod = cwAuthMethod
		}
		roleARN := awsRoleARN
		if cwRoleARN != "" {
			roleARN = cwRoleARN
		}
		if roleARN != "" {
			cwCfg.STS.RoleARN = roleARN
			if cwCfg.AuthMethod == "" || cwCfg.AuthMethod == "auto" {
				cwCfg.AuthMethod = "sts"
			}
		}

		// Initialize UI Theme & Colors
		ui.InitFromConfig(AppConfig.UI)
		// Redirect scan logging to keep TUI screen clean
		SilenceLogsForTUI()

		// If user specified region flag, use it, otherwise fall back to global config region
		region := cwRegion
		if region == "" && AppConfig != nil {
			if len(AppConfig.AWS.Regions) > 0 {
				region = AppConfig.AWS.Regions[0]
			}
		}
		if region == "" {
			region = "us-east-1" // ultimate default region
		}

		m, err := cwtui.NewModel(ctx, cwCfg, region, configFilePath(), AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing CloudWatch Logs TUI: %v\n", err)
			os.Exit(1)
		}

		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx))

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running CloudWatch Logs TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	cwCmd.Flags().StringVarP(&cwGroup, "group", "g", "", "Initial CloudWatch log group filter/pattern")
	cwCmd.Flags().StringVarP(&cwStream, "stream", "s", "", "Initial CloudWatch log stream filter")
	cwCmd.Flags().StringVarP(&cwFilter, "filter", "f", "", "Initial query pattern for log events")
	cwCmd.Flags().StringVar(&cwProfile, "profile", "", "AWS named profile (overrides global --profile)")
	cwCmd.Flags().StringVar(&cwAuthMethod, "auth-method", "", "Auth method: auto, profile, env, static, sts (overrides global --auth-method)")
	cwCmd.Flags().StringVar(&cwRoleARN, "role-arn", "", "IAM role ARN to assume via STS (overrides global --role-arn)")
	cwCmd.Flags().StringVar(&cwRegion, "region", "", "AWS region (overrides global configs/defaults)")
	cwCmd.Flags().StringVar(&cwTheme, "theme", "spotted-pardalote", "Color theme ("+strings.Join(ui.ThemeNames(), ", ")+")")
	cwCmd.Flags().BoolVar(&cwTui, "tui", true, "Launch in interactive TUI mode")
	rootCmd.AddCommand(cwCmd)
}
