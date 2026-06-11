package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/user/aws_explorer/internal/awsutil"
	"github.com/user/aws_explorer/internal/engine"
	"github.com/user/aws_explorer/internal/model"
	"github.com/user/aws_explorer/internal/tui"
	"github.com/user/aws_explorer/internal/ui"
)

var (
	snapshotPath string
	diffPaths    []string
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

		ui.InitFromConfig(AppConfig.UI)
		// The TUI owns the screen; keep scan logs from corrupting it.
		SilenceLogsForTUI()

		var seed []model.Resource
		offline := snapshotPath != "" || len(diffPaths) > 0
		if snapshotPath != "" {
			var err error
			seed, err = loadSnapshot(snapshotPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load snapshot: %v\n", err)
				os.Exit(1)
			}
		} else if len(diffPaths) > 0 {
			if len(diffPaths) != 2 {
				fmt.Fprintf(os.Stderr, "Error: --diff requires exactly two snapshot files\n")
				os.Exit(1)
			}
			oldRes, err := loadSnapshot(diffPaths[0])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load old snapshot: %v\n", err)
				os.Exit(1)
			}
			newRes, err := loadSnapshot(diffPaths[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load new snapshot: %v\n", err)
				os.Exit(1)
			}
			diff := awsutil.DiffScans(oldRes, newRes)
			seed = awsutil.BuildDiffResources(diff)
		}

		var m tea.Model
		if offline {
			// Offline view: no engine — no credentials, STS calls or region
			// discovery needed to browse a saved snapshot or a diff.
			m = tui.NewModelWithSeed(ctx, nil, configFilePath(), AppConfig, seed)
		} else {
			eng, err := engine.NewEngine(ctx, AppConfig)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to initialize engine: %v\n", err)
				os.Exit(1)
			}
			m = tui.NewModel(ctx, eng, configFilePath(), AppConfig)
		}

		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))

		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func loadSnapshot(path string) ([]model.Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var res []model.Resource
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, err
	}
	return res, nil
}

func init() {
	rootCmd.AddCommand(tuiCmd)
	tuiCmd.Flags().StringVar(&snapshotPath, "snapshot", "", "Path to a saved inventory snapshot JSON to view offline")
	tuiCmd.Flags().StringSliceVar(&diffPaths, "diff", nil, "Paths to two saved snapshots to diff (comma-separated or multiple flags)")
}
