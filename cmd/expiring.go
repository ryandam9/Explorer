package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/expiry"
	"github.com/ryandam9/aws_explorer/internal/output"
)

var expiringWithin string

var expiringCmd = &cobra.Command{
	Use:   "expiring",
	Short: "List everything that breaks on a calendar date",
	Long: `Expiring reports every deadline in the account, sorted by days remaining —
already-passed items first, with negative day counts:

  - ACM certificates approaching expiry (and whether they are in use)
  - Legacy IAM server certificates approaching expiry
  - Lambda functions on runtimes with an announced deprecation date
  - EKS clusters whose Kubernetes version is reaching end of standard support
  - RDS instances pinned to an expired CA certificate, and pending
    maintenance actions with an apply date
  - Secrets Manager secrets whose rotation is overdue

--within bounds the horizon (default 90 days); items already past are always
shown. Runtime/version end-of-life tables reflect AWS's published schedules
as of this release and are reviewed each release.

The report is read-only and best-effort: a denied API call skips that source
(reported on stderr) and never aborts the run.`,
	Example: `  # Everything breaking in the next 90 days, all regions
  aws_explorer expiring --all-regions

  # A tighter horizon
  aws_explorer expiring --within 30d

  # Machine-readable, e.g. page on anything within two weeks
  aws_explorer expiring -o json | jq '[.[] | select(.days <= 14)]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		within, err := parseWithin(expiringWithin)
		if err != nil {
			return err
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to initialize engine: %v\n", err)
			os.Exit(1)
		}
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		fmt.Fprintf(os.Stderr, "Checking %d region(s) for upcoming deadlines…\n", len(regions))

		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second
		items, errs := expiry.Collect(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout)
		items = expiry.Filter(items, within)

		output.PrintErrors(os.Stderr, errs)

		if len(items) == 0 && strings.EqualFold(outputFormat, "table") {
			fmt.Printf("Nothing expires within %d days.\n", within)
			return nil
		}
		if err := expiry.Render(os.Stdout, items, outputFormat, noHeader); err != nil {
			fmt.Fprintf(os.Stderr, "Error rendering report: %v\n", err)
			os.Exit(1)
		}
		return nil
	},
}

// parseWithin accepts a day count as "90", "90d", or any Go duration
// ("2160h"), returning whole days.
func parseWithin(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 90, nil
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
		if n < 0 {
			return 0, fmt.Errorf("--within must not be negative")
		}
		return n, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0, fmt.Errorf("--within must not be negative")
		}
		return int(d.Hours() / 24), nil
	}
	return 0, fmt.Errorf("invalid --within %q (use a day count like 90 or 90d)", s)
}

func init() {
	expiringCmd.Flags().StringVar(&expiringWithin, "within", "90d",
		"horizon for upcoming deadlines (e.g. 30d); already-passed items always show")
	rootCmd.AddCommand(expiringCmd)
}
