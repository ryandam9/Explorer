package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/ryandam9/aws_explorer/internal/clilog"
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
	relatedScan        string
)

var relatedCmd = &cobra.Command{
	Use:   "related <arn-or-id>",
	Short: "Related resources — what a resource uses and what uses it",
	Long: `Related shows everything linked to a resource, in both directions:

  Depends on →   what the resource uses (a Lambda's execution role, a volume's
                 KMS key, an instance's security groups …)
  Used by ←      what uses the resource (the where-used answer)

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
	Example: `  You can pass a full ARN, a bare resource id (sg-…, i-…, vol-…, eni-…,
  subnet-…, vpce-…, fs-…), a CloudWatch log-group name (/aws/lambda/…), or a
  bare name for IAM roles / S3 buckets / RDS / DynamoDB / SNS / SQS / etc.

  # ── By resource type ───────────────────────────────────────────────
  # Lambda function — its role, KMS key, subnets/SGs, log group, event sources
  aws_explorer related arn:aws:lambda:us-east-1:123456789012:function:checkout

  # EC2 instance — instance-profile role, subnet, AMI, key pair, ENIs
  aws_explorer related i-0abc123def456 -r us-east-1

  # Security group — everything it's attached to (ENIs, LBs, RDS, …)
  aws_explorer related sg-0abc123 --direction usedby

  # IAM role — what assumes it, and (with --uses) its policies/trust principals
  aws_explorer related arn:aws:iam::123456789012:role/app
  aws_explorer related app                       # bare role name also works

  # KMS key — every resource encrypted with it (a classic "safe to disable?")
  aws_explorer related arn:aws:kms:us-east-1:123456789012:key/abcd-1234 -r us-east-1

  # S3 bucket — event-notification targets, replication role, log bucket, SSE key
  aws_explorer related my-bucket
  aws_explorer related arn:aws:s3:::my-bucket

  # More: RDS/Aurora, DynamoDB, ELBv2, EFS, subnet, log group, SNS/SQS, …
  aws_explorer related orders-db -r us-east-1            # RDS instance
  aws_explorer related Orders -r us-east-1               # DynamoDB table
  aws_explorer related subnet-0abc123 --direction usedby -r us-east-1
  aws_explorer related fs-0abc123 -r us-east-1           # EFS file system
  aws_explorer related /aws/lambda/checkout -r us-east-1 # CloudWatch log group

  # ── Direction & depth ──────────────────────────────────────────────
  aws_explorer related sg-0abc123 --direction usedby     # only "used by"
  aws_explorer related my-fn --direction uses            # only "depends on"
  aws_explorer related arn:aws:iam::…:role/app --depth 2 # follow 2 hops out
  aws_explorer related arn:aws:iam::…:role/app --depth 3 --show-paths all

  # ── Scope & speed ──────────────────────────────────────────────────
  aws_explorer related sg-0abc123 --all-regions          # sweep every region
  aws_explorer related sg-0abc123 -r eu-west-1           # one region
  aws_explorer related arn:aws:kms:…:key/abcd --scan security   # focused scan
  aws_explorer related my-fn --scan lambda,kms,ec2       # explicit services
  aws_explorer related my-fn --cache-ttl 5m              # reuse a recent scan
  aws_explorer related my-fn --refresh                   # force a fresh scan
  aws_explorer related my-fn --debug-scan                # per-service timings

  # ── Output formats ─────────────────────────────────────────────────
  aws_explorer related sg-0abc123 -o json | jq '.used_by'
  aws_explorer related sg-0abc123 -o csv --no-header > links.csv
  aws_explorer related my-fn -o ndjson
  aws_explorer related my-fn --depth 2 --format mermaid > graph.md
  aws_explorer related my-fn --depth 2 --format dot | dot -Tpng -o graph.png

  # ── Decision helpers ───────────────────────────────────────────────
  aws_explorer related sg-0abc123 --direction usedby --risk   # blast-radius rating
  aws_explorer related arn:aws:kms:…:key/abcd --explain-scan   # what it would check
  aws_explorer related arn:aws:kms:…:key/abcd --tui            # interactive explorer

  # ── Common tasks ───────────────────────────────────────────────────
  # Before deleting a security group / KMS key / IAM role, across all regions:
  aws_explorer related sg-0abc123 --direction usedby --all-regions --risk
  aws_explorer related arn:aws:kms:…:key/abcd --depth 2 --all-regions
  # Debug an S3 → Lambda event pipeline, or a Lambda's event sources:
  aws_explorer related arn:aws:s3:::uploads --depth 2
  aws_explorer related arn:aws:lambda:…:function:worker --depth 2

  # See docs/related.md ("Examples by resource type") for a full table of
  # every resource type, an example invocation, and the links it reveals.`,
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
		scanServices, err := xref.ParseScan(relatedScan)
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

		// Status/diagnostic lines go to stderr, tinted by level (and with the
		// user's input highlighted) when stderr is a terminal — matching the
		// leveled slog stream. Piped/NO_COLOR output stays plain.
		color := clilog.ColorEnabled(isatty.IsTerminal(os.Stderr.Fd()))

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
		cacheKey := xref.CacheKey(version, eng.AccountID, AppConfig.AWS.Profile, append(regions, fmt.Sprintf("rp=%t", includeRolePolicies), "scan="+relatedScan))
		cachePath, _ := xref.CachePath(cacheKey)

		var edges []xref.Edge
		var errs []model.ExploreError
		cached := false
		if !relatedRefresh {
			if e, ok := xref.LoadCache(cachePath, version, cacheTTL, time.Now()); ok {
				edges, errs, cached = e.Edges, e.Errors, true
				clilog.Statusf(os.Stderr, color, "INFO", "Using cached scan from %s ago (--refresh to rescan)", time.Since(e.CreatedAt).Round(time.Second))
			}
		}
		if !cached {
			clilog.Statusf(os.Stderr, color, "INFO", "Scanning %d region(s) for resources related to %s…", len(regions), clilog.Highlight(args[0], color))
			var stats *xref.ScanStats
			edges, errs, stats = xref.CollectWithStats(ctx, eng.AWSConfig, regions, AppConfig.App.MaxConcurrency, timeout, includeRolePolicies, scanServices)
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
		result := xref.Related(args[0], xref.BuildForwardIndex(edges), xref.BuildIndex(edges), depth, allPaths).WithCollectionStatus(errs)
		if scanServices != nil {
			// Honesty (§8): a narrowed scan must narrow what it claims to have
			// checked.
			result.CheckedTypes = xref.CheckedTypesFor(result.Target.Kind, scanServices)
		}

		// Show the results first; the diagnostics (scan-scope notice, ambiguity
		// warning, collection-error summary, slow-scan hint) follow at the end so
		// they don't bury the report. This matches the streaming commands, which
		// also summarize errors after the rows.
		if err := xref.RenderRelated(os.Stdout, result, outFormat, noHeader, showUses, showUsedBy, result.Partial); err != nil {
			return fmt.Errorf("rendering report: %w", err)
		}
		// Deletion-risk estimate (#398): a human-only summary of the blast radius
		// (the "used by" side). Kept off machine formats so scripts are stable.
		if relatedRisk && showUsedBy && outFormat == "table" {
			a := xref.AssessRisk(result)
			fmt.Fprintf(os.Stdout, "\nDeletion risk: %s — %s.\n", a.Level, a.Reason)
		}

		if scanServices != nil {
			clilog.Statusf(os.Stderr, color, "WARNING", "Scan limited to --scan %s; coverage is narrower than a full scan.", relatedScan)
		}
		warnAmbiguousTarget(os.Stderr, args[0], edges, color)
		output.PrintErrors(os.Stderr, errs)
		if hint := timeoutHint(errs, arnRegionField(args[0]), len(regions)); hint != "" {
			fmt.Fprint(os.Stderr, hint)
		}
		return nil
	},
}

// arnRegionField extracts the region from an ARN (the 4th colon-field), or ""
// for a non-ARN / global resource — used to make the -r suggestion concrete.
func arnRegionField(s string) string {
	if !strings.HasPrefix(s, "arn:") {
		return ""
	}
	parts := strings.SplitN(s, ":", 6)
	if len(parts) >= 4 {
		return parts[3]
	}
	return ""
}

// timeoutHint returns scan-narrowing guidance when collection hit deadline/
// cancellation errors, so a timed-out (and thus incomplete) result tells the
// user how to get a complete, faster answer. region is the target's own region
// (for a concrete -r suggestion); regionCount lets us skip the -r tip when the
// scan was already a single region. Returns "" when nothing timed out.
func timeoutHint(errs []model.ExploreError, region string, regionCount int) string {
	timedOut := false
	for _, e := range errs {
		m := strings.ToLower(e.Message)
		if strings.Contains(m, "deadline exceeded") || strings.Contains(m, "canceled") || strings.Contains(m, "cancelled") {
			timedOut = true
			break
		}
	}
	if !timedOut {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nSome regions/services timed out, so the result above may be incomplete. To get a complete, faster answer, narrow the scan:\n")
	if regionCount != 1 {
		r := region
		if r == "" {
			r = "<region>"
		}
		fmt.Fprintf(&b, "  • -r %-12s scan only the resource's region (usually all you need for \"Depends on\")\n", r)
	}
	b.WriteString("  • --scan eventing  focus the \"Used by\" search (or pass an explicit service list)\n")
	b.WriteString("  • --debug-scan     show which regions/services are slow\n")
	return b.String()
}

// warnAmbiguousTarget prints a stderr warning when a bare-name query matches
// more than one fully-qualified resource by short form, so a merged graph isn't
// mistaken for one resource's (#386).
func warnAmbiguousTarget(w io.Writer, input string, edges []xref.Edge, color bool) {
	cands := xref.AmbiguousCandidates(input, edges)
	if len(cands) <= 1 {
		return
	}
	clilog.Statusf(w, color, "WARNING", "%s matches %d resources by name; results are merged. Pass a full ARN to disambiguate:",
		clilog.Highlight(input, color), len(cands))
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
	relatedCmd.Flags().StringVar(&relatedScan, "scan", "full", "scan scope: full|fast|security|eventing|network, or an explicit comma list of services")
	rootCmd.AddCommand(relatedCmd)
}
