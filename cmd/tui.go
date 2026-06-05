package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Start the interactive TUI mode",
	Long:  `Start the Text User Interface (TUI) for interactive exploration of AWS resources.`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Apply persistent CLI flag overrides (same as root command).
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

		tui.InitFromConfig(AppConfig.UI)

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize engine: %v\n", err)
			os.Exit(1)
		}

		m := tui.NewModel(ctx, eng, configFilePath(), AppConfig)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
