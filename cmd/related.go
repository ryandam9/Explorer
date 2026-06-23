package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/engine"
	"github.com/ryandam9/aws_explorer/internal/model"
	"github.com/ryandam9/aws_explorer/internal/output"
	"github.com/ryandam9/aws_explorer/internal/relatedtui"
	"github.com/ryandam9/aws_explorer/internal/ui"
	"github.com/ryandam9/aws_explorer/internal/xref"
)

const relatedMaxDepth = 5

var (
	relatedDepth       int
	relatedDirection   string
	relatedTUI         bool
	relatedShowPaths   string
	relatedCacheTTL    string
	relatedRefresh     bool
	relatedDebugScan   bool
	relatedFormat      string
	relatedRisk        bool
	relatedExplainScan bool
)

var relatedCmd = &cobra.Command{
	Use:   "related <arn-or-id>",
	Short: "Related resources — what a resource uses and what uses it",
	Long: `Related shows everything linked to a resource, in both directions:

  Uses (depends on) →   what the resource references (a Lambda's execution role,
                        a volume's KMS key, an instance's security groups …)
  Used by ←             what references the resource (the where-used answer)

It scans the account for the linking fields the inventory does not keep and
walks them from the resource you name. Pass a full ARN or a bare ID (an IAM
role name, an sg-… id, etc.). With --depth it follows links several hops out
(e.g. a Lambda → its role → that role's trust principals).

This is a resource-to-resource reference graph, not a container inventory:
querying a VPC/subnet/route-table id won't list what lives inside it (use
'aws_explorer vpc' for that). Identifiers it doesn't recognize as a supported
kind are still queried as raw links, flagged with a note.

Both directions are read-only and best-effort: a denied or failed API call
narrows what was checked (reported on stderr) and never aborts the run. The
report only reflects the relationship types this tool extracts, so an empty
side means "none of the collected link types" — not "this resource is isolated".

This generalizes 'whereused' (which answers only the "used by" direction).`,
	Example: `  # Everything linked to a Lambda function (both directions)
  aws_explorer related arn:aws:lambda:us-east-1:123456789012:function:checkout

  # Only what this security group is attached to
  aws_explorer related sg-0abc123 --direction usedby -r eu-west-1

  # Follow links two hops out, across all regions
  aws_explorer related arn:aws:iam::123456789012:role/app --depth 2 --all-regions

  # Machine-readable
  aws_explorer related sg-0abc123 -o json | jq '.uses'`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		showUses, showUsedBy, err := parseDirection(relatedDirection)
		if err != nil {
			return err
		}
		allPaths, err := parseShowPaths(relatedShowPaths)
		if err != nil {
			return err
		}
		outFormat, err := relatedOutputFormat(outputFormat, relatedFormat)
		if err != nil {
			return err
		}
		depth, err := parseDepth(relatedDepth)
		if err != nil {
			return err
		}

		// The TUI is a one-hop, two-pane step explorer; --depth / --direction
		// don't apply there, so reject them explicitly instead of accepting and
		// silently ignoring them (#382).
		if err := relatedTUIFlagError(relatedTUI, cmd.Flags().Changed("depth"), cmd.Flags().Changed("direction")); err != nil {
			return err
		}

		// --explain-scan describes which reference types the reverse ("used by")
		// scan checks for this target kind, without hitting AWS (#399).
		if relatedExplainScan {
			return explainScan(os.Stdout, args[0], xref.Classify(args[0]))
		}

		applyGlobalAWSOverrides()
		ctx := context.Background()

		eng, err := engine.NewEngine(ctx, AppConfig)
		if err != nil {
			return fmt.Errorf("failed to initialize engine: %w", err)
		}
		SilenceScanLogs()

		regions := eng.EffectiveRegions()
		timeout := time.Duration(AppConfig.App.TimeoutSeconds) * time.Second

		if relatedTUI {
			ui.InitFromConfig(AppConfig.UI)
			if idx, ok := ui.LookupTheme(resolveTheme(cmd, "")); ok {
				ui.SetActiveTheme(idx)
			}
			model := relatedtui.NewModel(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, allRegions || AppConfig.AWS.AllRegions, args[0])
			p := tea.NewProgram(ui.WithWindowTitle(model), tea.WithAltScreen(), tea.WithContext(ctx))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("running related explorer: %w", err)
			}
			return nil
		}

		cacheTTL, err := time.ParseDuration(relatedCacheTTL)
		if err != nil {
			return fmt.Errorf("invalid --cache-ttl %q: %w", relatedCacheTTL, err)
		}

		// The per-role IAM policy sweep is only needed when the query can land on
		// a role: the target itself is a role, or a multi-hop walk could reach
		// one. Skipping it for the common non-role, depth-1 case avoids the
		// expensive (and previously deadline-storming) sweep (§7).
		includeRolePolicies := depth > 1 || xref.Classify(args[0]).Kind == xref.KindIAMRole

		// Short-lived cache (#393): reuse a recent scan of this scope when
		// --cache-ttl is set, unless --refresh forces a live scan. Role-policy
		// edges change the graph shape, so they're part of the cache key.
		cacheKey := xref.CacheKey(version, eng.AccountID, AppConfig.AWS.Profile, append(regions, fmt.Sprintf("rp=%t", includeRolePolicies)))
		cachePath, _ := xref.CachePath(cacheKey)

		var edges []xref.Edge
		var errs []model.ExploreError
		cached := false
		if !relatedRefresh {
			if e, ok := xref.LoadCache(cachePath, version, cacheTTL, time.Now()); ok {
				edges, errs, cached = e.Edges, e.Errors, true
				fmt.Fprintf(os.Stderr, "Using cached scan from %s ago (--refresh to rescan)\n", time.Since(e.CreatedAt).Round(time.Second))
			}
		}
		if !cached {
			fmt.Fprintf(os.Stderr, "Scanning %d region(s) for resources related to %s…\n", len(regions), args[0])
			var stats *xref.ScanStats
			edges, errs, stats = xref.CollectWithStats(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, includeRolePolicies)
			if relatedDebugScan {
				fmt.Fprintln(os.Stderr, "Scan timings (per service, slowest first):")
				for _, line := range stats.Lines() {
					fmt.Fprintf(os.Stderr, "  %s\n", line)
				}
			}
			if cacheTTL > 0 {
				_ = xref.SaveCache(cachePath, xref.CacheEntry{Version: version, CreatedAt: time.Now(), Edges: edges, Errors: errs})
			}
		}
		output.PrintErrors(os.Stderr, errs)
		warnAmbiguousTarget(os.Stderr, args[0], edges)

		result := xref.Related(args[0], xref.BuildForwardIndex(edges), xref.BuildIndex(edges), depth, allPaths).WithCollectionStatus(errs)
		if err := xref.RenderRelated(os.Stdout, result, outFormat, noHeader, showUses, showUsedBy, result.Partial); err != nil {
			return fmt.Errorf("rendering report: %w", err)
		}
		// Deletion-risk estimate (#398): a human-only summary of the blast radius
		// (the "used by" side). Kept off machine formats so scripts are stable.
		if relatedRisk && showUsedBy && outFormat == "table" {
			a := xref.AssessRisk(result)
			fmt.Fprintf(os.Stdout, "\nDeletion risk: %s — %s.\n", a.Level, a.Reason)
		}
		return nil
	},
}

// warnAmbiguousTarget prints a stderr warning when a bare-name query matches
// more than one fully-qualified resource by short form, so a merged graph isn't
// mistaken for one resource's (#386).
func warnAmbiguousTarget(w io.Writer, input string, edges []xref.Edge) {
	cands := xref.AmbiguousCandidates(input, edges)
	if len(cands) <= 1 {
		return
	}
	fmt.Fprintf(w, "warning: %q matches %d resources by name; results are merged. Pass a full ARN to disambiguate:\n", input, len(cands))
	for _, c := range cands {
		fmt.Fprintf(w, "  - %s\n", c)
	}
}

// relatedTUIFlagError rejects flags the --tui explorer doesn't honor. The TUI
// walks a single hop per Enter and always shows both directions, so an
// explicitly-set --depth or --direction would otherwise be silently ignored
// (#382). depthSet/directionSet come from cmd.Flags().Changed so an explicit
// --depth 1 / --direction both (the defaults) is still accepted.
func relatedTUIFlagError(tui, depthSet, directionSet bool) error {
	if !tui {
		return nil
	}
	if depthSet {
		return fmt.Errorf("--depth is not used in --tui mode; the explorer walks one hop per Enter (use Enter to drill, Esc to go back)")
	}
	if directionSet {
		return fmt.Errorf("--direction is not used in --tui mode; the explorer always shows both directions")
	}
	return nil
}

// explainScan prints, without scanning, the reference types the reverse
// ("used by") query checks for the target's kind — the scoped list behind the
// honesty contract (#399).
func explainScan(w io.Writer, input string, t xref.Target) error {
	fmt.Fprintf(w, "Target: %s (%s)\n", input, t.Kind)
	types := xref.CheckedTypes(t.Kind)
	if len(types) == 0 {
		fmt.Fprintln(w, "This identifier isn't a first-class target kind (IAM role, KMS key, ACM")
		fmt.Fprintln(w, "certificate, security group). It is still queryable as raw graph links, but")
		fmt.Fprintln(w, "there is no scoped reference-type list for it.")
		return nil
	}
	fmt.Fprintln(w, "To find what uses it, related checks these reference types:")
	for _, ct := range types {
		fmt.Fprintf(w, "  - %s\n", ct)
	}
	return nil
}

// relatedOutputFormat resolves the effective output format. The graph dialects
// (#397) live behind --format (dot|mermaid) so they don't pollute the global
// -o validation; when --format is unset, -o (table/json/ndjson/csv) is used.
func relatedOutputFormat(output, graph string) (string, error) {
	if strings.TrimSpace(graph) == "" {
		return output, nil
	}
	switch strings.ToLower(strings.TrimSpace(graph)) {
	case "dot", "mermaid":
		return strings.ToLower(strings.TrimSpace(graph)), nil
	default:
		return "", fmt.Errorf("invalid --format %q; want dot or mermaid (use -o for table/json/ndjson/csv)", graph)
	}
}

// parseDepth normalizes and validates the --depth flag: values below 1 floor to
// a single hop, and anything beyond relatedMaxDepth is rejected.
func parseDepth(d int) (int, error) {
	if d > relatedMaxDepth {
		return 0, fmt.Errorf("--depth %d too large; maximum is %d", d, relatedMaxDepth)
	}
	if d < 1 {
		d = 1
	}
	return d, nil
}

// parseShowPaths maps the --show-paths flag to whether every distinct path to a
// resource is kept (true) or only the shortest (false, the default).
func parseShowPaths(s string) (allPaths bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "shortest":
		return false, nil
	case "all":
		return true, nil
	default:
		return false, fmt.Errorf("invalid --show-paths %q; want shortest or all", s)
	}
}

// parseDirection maps the --direction flag to which sections to show.
func parseDirection(d string) (showUses, showUsedBy bool, err error) {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", "both":
		return true, true, nil
	case "uses", "forward":
		return true, false, nil
	case "usedby", "used-by", "reverse":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("invalid --direction %q; want both, uses, or usedby", d)
	}
}

func init() {
	relatedCmd.Flags().IntVar(&relatedDepth, "depth", 1, fmt.Sprintf("how many hops to follow (1-%d)", relatedMaxDepth))
	relatedCmd.Flags().StringVar(&relatedDirection, "direction", "both", "which links to show: both, uses, usedby")
	relatedCmd.Flags().BoolVar(&relatedTUI, "tui", false, "open the interactive related-resources explorer")
	relatedCmd.Flags().StringVar(&relatedShowPaths, "show-paths", "shortest", "for multi-hop results: shortest (one path per resource) or all")
	relatedCmd.Flags().StringVar(&relatedCacheTTL, "cache-ttl", "0", "reuse a cached scan younger than this (e.g. 5m); 0 disables caching")
	relatedCmd.Flags().BoolVar(&relatedRefresh, "refresh", false, "ignore any cached scan and re-query AWS (still writes the cache)")
	relatedCmd.Flags().BoolVar(&relatedDebugScan, "debug-scan", false, "print per-service scan timings to stderr")
	relatedCmd.Flags().StringVar(&relatedFormat, "format", "", "graph export format: dot or mermaid (overrides -o)")
	relatedCmd.Flags().BoolVar(&relatedRisk, "risk", false, "print a deletion-risk estimate from the blast radius (table output)")
	relatedCmd.Flags().BoolVar(&relatedExplainScan, "explain-scan", false, "list the reference types checked for this target, without scanning AWS")
	rootCmd.AddCommand(relatedCmd)
}
