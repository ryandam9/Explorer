package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/ecstriage"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/output"
)

var ecsStoppedCluster string

// ecsCmd groups ECS-specific subcommands.
var ecsCmd = &cobra.Command{
	Use:   "ecs",
	Short: "ECS triage helpers",
	Long:  `ECS-specific subcommands. Currently: "stopped" triages recently stopped tasks.`,
}

var ecsStoppedCmd = &cobra.Command{
	Use:   "stopped",
	Short: `Triage recently stopped ECS tasks ("why did my task stop?")`,
	Long: `Stopped answers the perennial "why did my task stop?" ticket. For each
recently stopped task it prints the task-level stop reason and the failing
container's exit code, with the exit code translated into plain English:

  - 137 → possible OOM-kill (128+SIGKILL); raise memory or fix the leak
  - 139 → segfault (128+SIGSEGV)
  - 143 → SIGTERM (often a normal shutdown)
  - container reason mentioning memory → out-of-memory

It scans every cluster in scope by default, or just one with --cluster (name
or ARN). The report is read-only and best-effort: a denied or failed API call
skips that region (reported on stderr) and never aborts the run.

Note: ECS retains stopped tasks for roughly one hour. An empty report means
nothing stopped in that window, not that nothing ever fails — run it soon
after the failure.

Needs ecs:ListClusters, ecs:ListTasks and ecs:DescribeTasks.`,
	Example: `  # Triage stopped tasks across all in-scope regions
  aws_explorer ecs stopped --all-regions

  # Just one cluster
  aws_explorer ecs stopped --cluster my-cluster -r us-east-1

  # Machine-readable; find the OOM-kills
  aws_explorer ecs stopped -o json | jq '[.[] | select(.exit_code == 137)]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		fmt.Fprintf(os.Stderr, "Checking %d region(s) for recently stopped ECS tasks…\n", len(regions))

		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second
		recs, errs := ecstriage.Collect(ctx, eng.AWSConfig, regions, ecsStoppedCluster, AppConfig.App.MaxConcurrency, timeout)

		output.PrintErrors(os.Stderr, errs)

		if len(recs) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Println("No stopped tasks found in the lookup window (ECS keeps them ~1h).")
			return nil
		}
		if err := ecstriage.Render(os.Stdout, recs, outputFormat, noHeader); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering report: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	ecsStoppedCmd.Flags().StringVar(&ecsStoppedCluster, "cluster", "",
		"limit to one cluster (name or ARN); default scans every cluster in scope")
	ecsCmd.AddCommand(ecsStoppedCmd)
	rootCmd.AddCommand(ecsCmd)
}
