package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/awsutil"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/tui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	snapshotPath string
	diffPaths    []string
)

var snapshotDiffCmd = &cobra.Command{
	Use:   "snapshot-diff",
	Short: "Browse a saved inventory snapshot, or diff two snapshots, offline",
	Long: `snapshot-diff opens the interactive TUI over saved inventory snapshots —
no AWS credentials, STS calls or region discovery needed.

Pass --snapshot to browse a single saved snapshot, or --diff to compare two
snapshots and explore what was added, removed or modified between them.
Snapshots are the JSON written by 'summary -o json'.

To explore live AWS resources interactively, use 'summary --tui' instead.`,
	Example: `  # Browse a saved snapshot offline (no credentials needed)
  aws_explorer snapshot-diff --snapshot inventory.json

  # Diff two snapshots
  aws_explorer snapshot-diff --diff before.json,after.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Offline-only: one of the two inputs is required. Live exploration of
		// the account lives in 'summary --tui'.
		if snapshotPath == "" && len(diffPaths) == 0 {
			return fmt.Errorf("snapshot-diff needs --snapshot <file> or --diff <old,new>; " +
				"to explore live resources use 'summary --tui'")
		}
		if snapshotPath != "" && len(diffPaths) > 0 {
			return fmt.Errorf("--snapshot and --diff are mutually exclusive")
		}

		applyGlobalAWSOverrides()
		ui.InitFromConfig(AppConfig.UI)

		var seed []model.Resource
		if snapshotPath != "" {
			res, err := loadSnapshot(snapshotPath)
			if err != nil {
				return fmt.Errorf("loading snapshot: %w", err)
			}
			seed = res
		} else {
			if len(diffPaths) != 2 {
				return fmt.Errorf("--diff requires exactly two snapshot files")
			}
			oldRes, err := loadSnapshot(diffPaths[0])
			if err != nil {
				return fmt.Errorf("loading old snapshot: %w", err)
			}
			newRes, err := loadSnapshot(diffPaths[1])
			if err != nil {
				return fmt.Errorf("loading new snapshot: %w", err)
			}
			seed = awsutil.BuildDiffResources(awsutil.DiffScans(oldRes, newRes))
		}

		// No engine: browsing saved JSON needs no credentials, STS calls or
		// region discovery.
		m := tui.NewModelWithSeed(ctx, nil, configFilePath(), AppConfig, seed)
		p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running snapshot-diff TUI: %w", err)
		}
		return nil
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
	rootCmd.AddCommand(snapshotDiffCmd)
	snapshotDiffCmd.Flags().StringVar(&snapshotPath, "snapshot", "", "Path to a saved inventory snapshot JSON to view offline")
	snapshotDiffCmd.Flags().StringSliceVar(&diffPaths, "diff", nil, "Paths to two saved snapshots to diff (comma-separated or multiple flags)")
}
