package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/acctsnap"
	"github.com/ryandam9/aws_explorer/internal/discovery"
	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/summary"
	"github.com/ryandam9/aws_explorer/internal/tui"
	"github.com/ryandam9/aws_explorer/internal/ui"
)

var (
	summaryTUI       bool
	summaryTypedOnly bool
	summaryBaseline  bool
	summaryDiff      bool
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "List every AWS resource across all regions",
	Long: `Summary lists all discoverable AWS resources across every configured
region as a single numbered inventory. Each row shows the serial number, the
resource name (or "-" when it has none), the resource type, the ARN, and the
region (with availability zone when the resource is zonal).

By default the inventory is printed as a table (use -o json|ndjson|csv for
other formats). Pass --tui to explore the same data interactively.

--baseline saves the inventory as the account's baseline snapshot
(~/.aws_explorer/account-snapshots/<account-id>/); --diff scans again later
and reports what was added, removed, or modified since — "what changed in
this account since yesterday". Baselines are keyed by account and region
scope, and only stable fields (name, state, tags) are compared, so an
unchanged account diffs clean.`,
	Example: `  # Full inventory of every region
  aws_explorer summary --all-regions

  # One region, exported as CSV
  aws_explorer summary -r us-east-1 -o csv > inventory.csv

  # Explore interactively
  aws_explorer summary --tui

  # What changed in this account since yesterday?
  aws_explorer summary --baseline            # yesterday
  aws_explorer summary --diff                # today
  aws_explorer summary --diff -o json        # for automation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if summaryBaseline && summaryDiff {
			return fmt.Errorf("--baseline and --diff are mutually exclusive")
		}
		if (summaryBaseline || summaryDiff) && summaryTUI {
			return fmt.Errorf("--baseline/--diff cannot be combined with --tui (use the D key in the TUI instead)")
		}

		applyGlobalAWSOverrides()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}

		if summaryTUI {
			ui.InitFromConfig(AppConfig.UI)
			// The TUI owns the screen; keep scan logs from corrupting it.
			SilenceScanLogs()
			// Gather the all-services sweep up front and seed the TUI with it;
			// the typed collectors then stream in and merge (deduped by ARN).
			var seed []model.Resource
			if !summaryTypedOnly {
				fmt.Fprintln(os.Stderr, "Discovering resources across all services…")
				seed, _ = discoverAllAccounts(ctx, eng, eng.EffectiveRegions(), AppConfig.App.MaxConcurrency)
			}
			m := tui.NewModelWithSeed(ctx, eng, configFilePath(), AppConfig, seed,
				tui.WithCoverageAdvisory(!summaryTypedOnly))
			p := tea.NewProgram(ui.WithWindowTitle(m), tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("running summary TUI: %w", err)
			}
			return nil
		}

		// Problems are summarized after the run; the raw log stream would
		// only interleave with the table.
		SilenceScanLogs()

		result, err := eng.Run(ctx)
		if err != nil {
			return fmt.Errorf("collecting resources: %w", err)
		}

		resources := result.Resources
		errs := result.Errors

		// Universal sweep across all services via the Resource Groups Tagging
		// API, merged with the rich typed collectors above (deduped by ARN). Run
		// it per configured account — the same fan-out the typed collectors use —
		// so multi-account summaries are complete and consistently labelled (#359).
		if !summaryTypedOnly {
			discovered, dErrs := discoverAllAccounts(
				ctx, eng, eng.EffectiveRegions(), AppConfig.App.MaxConcurrency)
			resources = append(resources, discovered...)
			errs = append(errs, dErrs...)
		}

		output.PrintErrors(os.Stderr, errs)

		if summaryBaseline || summaryDiff {
			if err := runBaselineOrDiff(resources, eng.AccountID, eng.EffectiveRegions(), summaryDiff); err != nil {
				return err
			}
			return nil
		}

		rows := summary.BuildRows(resources)
		if len(rows) == 0 {
			// Name the scanned scope so "none" is diagnosable (§3).
			fmt.Printf("No resources found in %s.\n", regionScopeLabel(eng.EffectiveRegions()))
		} else if err := summary.Render(os.Stdout, rows, outputFormat, noHeader); err != nil {
			return fmt.Errorf("rendering summary: %w", err)
		}

		// Coverage advisory: only for the human table view (json/csv/ndjson must
		// stay machine-clean). It lists the common services that showed nothing
		// and reminds the user, in plain terms, that an untagged resource can be
		// missing — the usual reason an inventory looks short.
		if isTableFormat(outputFormat) {
			cov := summary.Coverage(resources, eng.TypedServices(),
				AppConfig.Summary.CommonServices, AppConfig.Summary.HideServices)
			if note := summary.CoverageNote(cov, !summaryTypedOnly); note != "" {
				fmt.Fprintln(os.Stdout, "\n"+note)
			}
		}
		return nil
	},
}

// discoverAllAccounts runs the Resource Groups Tagging API sweep once per
// configured account (the same fan-out the typed collectors use), stamping each
// account's identifier consistently so multi-account summaries are complete and
// dedupe/labels line up with typed results (#359). In single-account mode this
// is one pass, behaving as before.
func discoverAllAccounts(ctx context.Context, eng *engine.Engine, regions []string, maxConcurrency int) ([]model.Resource, []model.ExploreError) {
	multiAcct := AppConfig != nil && len(AppConfig.Accounts) > 0
	var resources []model.Resource
	var errs []model.ExploreError
	for _, sw := range eng.AccountSweeps(ctx) {
		if sw.Err != nil {
			// A bad account narrows coverage with a visible note; it never hides
			// the other accounts (§3/§6a).
			errs = append(errs, model.ExploreError{
				Service: "account:" + sw.Name, Code: "AccountConfig",
				Message: fmt.Sprintf("account %q skipped: %v", sw.Name, sw.Err),
			})
			continue
		}
		discovered, dErrs := discovery.Discover(ctx, sw.AWSConfig, regions, maxConcurrency)
		// Match the typed-collector stamping: account *name* in multi-account
		// mode, resolved account *ID* in single-account mode (engine.go).
		acct := sw.AccountID
		if multiAcct {
			acct = sw.Name
		}
		stampAccount(discovered, acct)
		resources = append(resources, discovered...)
		errs = append(errs, dErrs...)
	}
	return resources, errs
}

// stampAccount sets AccountID on every resource (when acct is non-empty), so
// tag-discovered resources carry the same account identifier the typed
// collectors stamp.
func stampAccount(resources []model.Resource, acct string) {
	if acct == "" {
		return
	}
	for i := range resources {
		resources[i].AccountID = acct
	}
}

// isTableFormat reports whether fmt is the human table view — i.e. not one of
// the machine-readable formats, which must stay free of advisory text.
func isTableFormat(format string) bool {
	switch strings.ToLower(format) {
	case "json", "ndjson", "csv":
		return false
	default:
		return true
	}
}

// runBaselineOrDiff saves the scan as the account baseline, or diffs the scan
// against the saved baseline, depending on diff.
func runBaselineOrDiff(resources []model.Resource, accountID string, regions []string, diff bool) error {
	live := acctsnap.New(resources, accountID, regions)

	if !diff {
		path, err := acctsnap.Save(live)
		if err != nil {
			return fmt.Errorf("saving baseline: %w", err)
		}
		fmt.Printf("Baseline saved: %s (%d resources) — run 'summary --diff' later to see what changed.\n",
			path, len(live.Entries))
		return nil
	}

	baseline, ok, err := acctsnap.Load(accountID, regions)
	if err != nil {
		return fmt.Errorf("loading baseline: %w", err)
	}
	if !ok {
		// A baseline under a different region scope would diff as a flood of
		// bogus removals; refuse and say which scopes exist instead.
		if scopes := acctsnap.Scopes(accountID); len(scopes) > 0 {
			return fmt.Errorf("no baseline for region scope %q — baselines exist for: %s (rerun with the matching regions, or save a new baseline with --baseline)",
				acctsnap.ScopeKey(regions), strings.Join(scopes, ", "))
		}
		return fmt.Errorf("no baseline saved for this account yet — run 'summary --baseline' first")
	}

	rep := acctsnap.NewReport(baseline, acctsnap.Diff(baseline, live))
	return acctsnap.Render(os.Stdout, rep, outputFormat, noHeader)
}

func init() {
	summaryCmd.Flags().BoolVar(&summaryTUI, "tui", false, "Explore the inventory interactively instead of printing a table")
	summaryCmd.Flags().BoolVar(&summaryTypedOnly, "typed-only", false, "Only use the built-in typed collectors; skip the all-services Tagging API sweep")
	summaryCmd.Flags().BoolVar(&summaryBaseline, "baseline", false, "Save this scan as the account's baseline snapshot")
	summaryCmd.Flags().BoolVar(&summaryDiff, "diff", false, "Diff this scan against the saved baseline (what changed since)")
	rootCmd.AddCommand(summaryCmd)
}
